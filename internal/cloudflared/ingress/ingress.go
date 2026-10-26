package ingress

import (
	"fmt"
	"net"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"golang.org/x/net/idna"

	"go.mewis.me/cf-tunnel/internal/cloudflared/config"
	"go.mewis.me/cf-tunnel/internal/cloudflared/ingress/middleware"
	"go.mewis.me/cf-tunnel/internal/cloudflared/ipaccess"
)

var (
	ErrNoIngressRules             = errors.New("The config file doesn't contain any ingress rules")
	ErrNoIngressRulesCLI          = errors.New("No ingress rules were defined in provided config (if any) nor from the cli, cloudflared will return 503 for all incoming HTTP requests")
	errLastRuleNotCatchAll        = errors.New("The last ingress rule must match all URLs (i.e. it should not have a hostname or path filter)")
	errBadWildcard                = errors.New("Hostname patterns can have at most one wildcard character (\"*\") and it can only be used for subdomains, e.g. \"*.example.com\"")
	errHostnameContainsPort       = errors.New("Hostname cannot contain a port")
	ErrURLIncompatibleWithIngress = errors.New("You can't set the --url flag (or $TUNNEL_URL) when using multiple-origin ingress rules")
)

const (
	ServiceBastion     = "bastion"
	ServiceSocksProxy  = "socks-proxy"
	ServiceWarpRouting = "warp-routing"
)

// FindMatchingRule returns the index of the Ingress Rule which matches the given
// hostname and path. This function assumes the last rule matches everything,
// which is the case if the rules were instantiated via the ingress#Validate method.
//
// Negative index rule signifies local cloudflared rules (not-user defined).
func (ing Ingress) FindMatchingRule(hostname, path string) (*Rule, int) {
	// The hostname might contain port. We only want to compare the host part with the rule
	host, _, err := net.SplitHostPort(hostname)
	if err == nil {
		hostname = host
	}
	// Normalize once before first-match routing when Cloudflare Access
	// middleware is configured; otherwise traversal can bypass protected rules.
	// Only the value used for route selection changes, not the origin request.
	if rulesRequireAccess(ing.InternalRules) || rulesRequireAccess(ing.Rules) {
		path = canonicalPathForMatching(path)
	}
	for i, rule := range ing.InternalRules {
		if rule.Matches(hostname, path) {
			// Local rule matches return a negative rule index to distiguish local rules from user-defined rules in logs
			// Full range would be [-1 .. )
			return &rule, -1 - i
		}
	}
	for i, rule := range ing.Rules {
		if rule.Matches(hostname, path) {
			return &rule, i
		}
	}

	i := len(ing.Rules) - 1
	return &ing.Rules[i], i
}

func rulesRequireAccess(rules []Rule) bool {
	for _, rule := range rules {
		if rule.Config.Access.Required {
			return true
		}
	}
	return false
}

func canonicalPathForMatching(p string) string {
	if p == "" {
		return "/"
	}
	p = strings.ReplaceAll(p, `\`, "/")
	trailingSlash := strings.HasSuffix(p, "/") || strings.HasSuffix(p, "/.") || strings.HasSuffix(p, "/..")
	cleaned := path.Clean(p)
	if trailingSlash && cleaned != "/" {
		cleaned += "/"
	}
	return cleaned
}

func matchHost(ruleHost, reqHost string) bool {
	if ruleHost == reqHost {
		return true
	}

	// Validate hostnames that use wildcards at the start
	if strings.HasPrefix(ruleHost, "*.") {
		toMatch := strings.TrimPrefix(ruleHost, "*")
		return strings.HasSuffix(reqHost, toMatch)
	}
	return false
}

// Ingress maps eyeball requests to origins.
type Ingress struct {
	// Set of ingress rules that are not added to remote config, e.g. management
	InternalRules []Rule
	// Rules that are provided by the user from remote or local configuration
	Rules    []Rule              `json:"ingress"`
	Defaults OriginRequestConfig `json:"originRequest"`
}

// ParseIngress parses ingress rules, but does not send HTTP requests to the origins.
func ParseIngress(conf *config.Configuration) (Ingress, error) {
	if conf == nil || len(conf.Ingress) == 0 {
		return Ingress{}, ErrNoIngressRules
	}
	return validateIngress(conf.Ingress, originRequestFromConfig(conf.OriginRequest))
}

// IsEmpty checks if there are any ingress rules.
func (ing Ingress) IsEmpty() bool {
	return len(ing.Rules) == 0
}

// IsSingleRule checks if the user only specified a single ingress rule.
func (ing Ingress) IsSingleRule() bool {
	return len(ing.Rules) == 1
}

// StartOrigins will start any origin services managed by cloudflared, e.g. proxy servers or Hello World.
func (ing Ingress) StartOrigins(
	log *zerolog.Logger,
	shutdownC <-chan struct{},
) error {
	for _, rule := range ing.Rules {
		if err := rule.Service.start(log, shutdownC, rule.Config); err != nil {
			return errors.Wrapf(err, "Error starting local service %s", rule.Service)
		}
	}
	return nil
}

// CatchAll returns the catch-all rule (i.e. the last rule)
func (ing Ingress) CatchAll() *Rule {
	return &ing.Rules[len(ing.Rules)-1]
}

// Gets the default ingress rule that will be return 503 status
// code for all incoming requests.
func GetDefaultIngressRules(log *zerolog.Logger) []Rule {
	noRulesService := newDefaultStatusCode(log)
	return []Rule{
		{
			Service: &noRulesService,
		},
	}
}

func validateAccessConfiguration(cfg *config.AccessConfig) error {
	if !cfg.Required {
		return nil
	}

	// we allow for an initial setup where user can force Access but not configure the rest of the keys.
	// however, if the user specified audTags but forgot teamName, we should alert it.
	if cfg.TeamName == "" && len(cfg.AudTag) > 0 {
		return errors.New("access.TeamName cannot be blank when access.audTags are present")
	}

	return nil
}

func validateIngress(ingress []config.UnvalidatedIngressRule, defaults OriginRequestConfig) (Ingress, error) {
	rules := make([]Rule, len(ingress))
	for i, r := range ingress {
		cfg := setConfig(defaults, r.OriginRequest)
		var service OriginService

		if prefix := "unix:"; strings.HasPrefix(r.Service, prefix) {
			// No validation necessary for unix socket filepath services
			path := strings.TrimPrefix(r.Service, prefix)
			service = &unixSocketPath{path: path, scheme: "http"}
		} else if prefix := "unix+tls:"; strings.HasPrefix(r.Service, prefix) {
			path := strings.TrimPrefix(r.Service, prefix)
			service = &unixSocketPath{path: path, scheme: "https"}
		} else if prefix := "http_status:"; strings.HasPrefix(r.Service, prefix) {
			statusCode, err := strconv.Atoi(strings.TrimPrefix(r.Service, prefix))
			if err != nil {
				return Ingress{}, errors.Wrap(err, "invalid HTTP status code")
			}
			if statusCode < 100 || statusCode > 999 {
				return Ingress{}, fmt.Errorf("invalid HTTP status code: %d", statusCode)
			}
			srv := newStatusCode(statusCode)
			service = &srv
		} else if r.Service == HelloWorldFlag || r.Service == HelloWorldService {
			service = new(helloWorld)
		} else if r.Service == ServiceSocksProxy {
			rules := make([]ipaccess.Rule, len(r.OriginRequest.IPRules))

			for i, ipRule := range r.OriginRequest.IPRules {
				rule, err := ipaccess.NewRuleByCIDR(ipRule.Prefix, ipRule.Ports, ipRule.Allow)
				if err != nil {
					return Ingress{}, fmt.Errorf("unable to create ip rule for %s: %s", r.Service, err)
				}
				rules[i] = rule
			}

			accessPolicy, err := ipaccess.NewPolicy(false, rules)
			if err != nil {
				return Ingress{}, fmt.Errorf("unable to create ip access policy for %s: %s", r.Service, err)
			}

			service = newSocksProxyOverWSService(accessPolicy)
		} else if r.Service == ServiceBastion || cfg.BastionMode {
			// Bastion mode will always start a Websocket proxy server, which will
			// overwrite the localService.URL field when `start` is called. So,
			// leave the URL field empty for now.
			cfg.BastionMode = true
			service = newBastionService()
		} else {
			// Validate URL services
			u, err := url.Parse(r.Service)
			if err != nil {
				return Ingress{}, err
			}

			if u.Scheme == "" || u.Hostname() == "" {
				return Ingress{}, fmt.Errorf("%s is an invalid address, please make sure it has a scheme and a hostname", r.Service)
			}

			if u.Path != "" {
				return Ingress{}, fmt.Errorf("%s is an invalid address, ingress rules don't support proxying to a different path on the origin service. The path will be the same as the eyeball request's path", r.Service)
			}
			if isHTTPService(u) {
				service = &httpService{url: u}
			} else {
				service = newTCPOverWSService(u)
			}
		}

		var handlers []middleware.Handler
		if access := r.OriginRequest.Access; access != nil {
			if err := validateAccessConfiguration(access); err != nil {
				return Ingress{}, err
			}
			if access.Required {
				verifier := middleware.NewJWTValidator(access.TeamName, access.Environment, access.AudTag)
				handlers = append(handlers, verifier)
			}
		}

		if err := validateHostname(r, i, len(ingress)); err != nil {
			return Ingress{}, err
		}

		isCatchAllRule := (r.Hostname == "" || r.Hostname == "*") && r.Path == ""
		punycodeHostname := ""
		if !isCatchAllRule {
			punycode, err := idna.Lookup.ToASCII(r.Hostname)
			// Don't provide the punycode hostname if it is the same as the original hostname
			if err == nil && punycode != r.Hostname {
				punycodeHostname = punycode
			}
		}

		var pathRegexp *Regexp
		if r.Path != "" {
			var err error
			regex, err := regexp.Compile(r.Path)
			if err != nil {
				return Ingress{}, errors.Wrapf(err, "Rule #%d has an invalid regex", i+1)
			}
			pathRegexp = &Regexp{Regexp: regex}
		}

		rules[i] = Rule{
			Hostname:         r.Hostname,
			punycodeHostname: punycodeHostname,
			Service:          service,
			Path:             pathRegexp,
			Handlers:         handlers,
			Config:           cfg,
		}
	}
	return Ingress{Rules: rules, Defaults: defaults}, nil
}

func validateHostname(r config.UnvalidatedIngressRule, ruleIndex, totalRules int) error {
	// Ensure that the hostname doesn't contain port
	_, _, err := net.SplitHostPort(r.Hostname)
	if err == nil {
		return errHostnameContainsPort
	}
	// Ensure that there are no wildcards anywhere except the first character
	// of the hostname.
	if strings.LastIndex(r.Hostname, "*") > 0 {
		return errBadWildcard
	}

	// The last rule should catch all hostnames.
	isCatchAllRule := (r.Hostname == "" || r.Hostname == "*") && r.Path == ""
	isLastRule := ruleIndex == totalRules-1
	if isLastRule && !isCatchAllRule {
		return errLastRuleNotCatchAll
	}
	// ONLY the last rule should catch all hostnames.
	if !isLastRule && isCatchAllRule {
		return ruleShouldNotBeCatchAllError{index: ruleIndex, hostname: r.Hostname}
	}
	return nil
}

type ruleShouldNotBeCatchAllError struct {
	index    int
	hostname string
}

func (e ruleShouldNotBeCatchAllError) Error() string {
	return fmt.Sprintf("Rule #%d is matching the hostname '%s', but "+
		"this will match every hostname, meaning the rules which follow it "+
		"will never be triggered.", e.index+1, e.hostname)
}

func isHTTPService(url *url.URL) bool {
	return url.Scheme == "http" || url.Scheme == "https" || url.Scheme == "ws" || url.Scheme == "wss"
}
