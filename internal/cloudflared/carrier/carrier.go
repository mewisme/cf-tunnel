//lint:file-ignore ST1005 Pinned upstream error text.
package carrier

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const cfJumpDestinationHeader = "Cf-Access-Jump-Destination"

func ResolveBastionDest(r *http.Request) (string, error) {
	jumpDestination := r.Header.Get(cfJumpDestinationHeader)
	if jumpDestination == "" {
		return "", fmt.Errorf("Did not receive final destination from client. The --destination flag is likely not set on the client side")
	}
	if jumpURL, err := url.Parse(jumpDestination); err == nil && jumpURL.Host != "" {
		return strings.SplitN(jumpURL.Host, "/", 2)[0], nil
	}
	return strings.SplitN(jumpDestination, "/", 2)[0], nil
}
