package config

import (
	"encoding/json"
	"strconv"
	"time"
)

const (
	BastionFlag = "bastion"
)

type UnvalidatedIngressRule struct {
	Hostname      string              `json:"hostname,omitempty"`
	Path          string              `json:"path,omitempty"`
	Service       string              `json:"service,omitempty"`
	OriginRequest OriginRequestConfig `yaml:"originRequest" json:"originRequest"`
}

type OriginRequestConfig struct {
	ConnectTimeout         *CustomDuration `yaml:"connectTimeout" json:"connectTimeout,omitempty"`
	TLSTimeout             *CustomDuration `yaml:"tlsTimeout" json:"tlsTimeout,omitempty"`
	TCPKeepAlive           *CustomDuration `yaml:"tcpKeepAlive" json:"tcpKeepAlive,omitempty"`
	NoHappyEyeballs        *bool           `yaml:"noHappyEyeballs" json:"noHappyEyeballs,omitempty"`
	KeepAliveConnections   *int            `yaml:"keepAliveConnections" json:"keepAliveConnections,omitempty"`
	KeepAliveTimeout       *CustomDuration `yaml:"keepAliveTimeout" json:"keepAliveTimeout,omitempty"`
	HTTPHostHeader         *string         `yaml:"httpHostHeader" json:"httpHostHeader,omitempty"`
	OriginServerName       *string         `yaml:"originServerName" json:"originServerName,omitempty"`
	MatchSNIToHost         *bool           `yaml:"matchSNItoHost" json:"matchSNItoHost,omitempty"`
	CAPool                 *string         `yaml:"caPool" json:"caPool,omitempty"`
	NoTLSVerify            *bool           `yaml:"noTLSVerify" json:"noTLSVerify,omitempty"`
	DisableChunkedEncoding *bool           `yaml:"disableChunkedEncoding" json:"disableChunkedEncoding,omitempty"`
	BastionMode            *bool           `yaml:"bastionMode" json:"bastionMode,omitempty"`
	ProxyAddress           *string         `yaml:"proxyAddress" json:"proxyAddress,omitempty"`
	ProxyPort              *uint           `yaml:"proxyPort" json:"proxyPort,omitempty"`
	ProxyType              *string         `yaml:"proxyType" json:"proxyType,omitempty"`
	IPRules                []IngressIPRule `yaml:"ipRules" json:"ipRules,omitempty"`
	Http2Origin            *bool           `yaml:"http2Origin" json:"http2Origin,omitempty"`
	Access                 *AccessConfig   `yaml:"access" json:"access,omitempty"`
}

type AccessConfig struct {
	Required    bool     `yaml:"required" json:"required,omitempty"`
	TeamName    string   `yaml:"teamName" json:"teamName"`
	AudTag      []string `yaml:"audTag" json:"audTag"`
	Environment string   `yaml:"environment" json:"environment,omitempty"`
}

type IngressIPRule struct {
	Prefix *string `yaml:"prefix" json:"prefix"`
	Ports  []int   `yaml:"ports" json:"ports"`
	Allow  bool    `yaml:"allow" json:"allow"`
}

type Configuration struct {
	TunnelID      string `yaml:"tunnel"`
	Ingress       []UnvalidatedIngressRule
	WarpRouting   WarpRoutingConfig   `yaml:"warp-routing"`
	OriginRequest OriginRequestConfig `yaml:"originRequest"`
	sourceFile    string
}

func (c *Configuration) Source() string {
	return c.sourceFile
}

type WarpRoutingConfig struct {
	ConnectTimeout *CustomDuration `yaml:"connectTimeout" json:"connectTimeout,omitempty"`
	MaxActiveFlows *uint64         `yaml:"maxActiveFlows" json:"maxActiveFlows,omitempty"`
	TCPKeepAlive   *CustomDuration `yaml:"tcpKeepAlive" json:"tcpKeepAlive,omitempty"`
}

type CustomDuration struct {
	time.Duration
}

func (s CustomDuration) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.Duration.Seconds())
}

func (s *CustomDuration) UnmarshalJSON(data []byte) error {
	seconds, err := strconv.ParseInt(string(data), 10, 64)
	if err != nil {
		return err
	}
	s.Duration = time.Duration(seconds * int64(time.Second))
	return nil
}

func (s *CustomDuration) MarshalYAML() (interface{}, error) {
	return s.Duration.String(), nil
}

func (s *CustomDuration) UnmarshalYAML(unmarshal func(interface{}) error) error {
	return unmarshal(&s.Duration)
}
