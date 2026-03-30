package domaincheck

import (
	"context"
	"net"
	"strings"
)

type Result struct {
	Domain       string `json:"domain"`
	DKIMSelector string `json:"dkim_selector"`
	SPFValid     bool   `json:"spf_valid"`
	DKIMValid    bool   `json:"dkim_valid"`
	DMARCValid   bool   `json:"dmarc_valid"`
	Ready        bool   `json:"ready"`
}

type Checker struct {
	resolver *net.Resolver
}

func New() *Checker {
	return &Checker{resolver: net.DefaultResolver}
}

func (c *Checker) Check(ctx context.Context, domain, dkimSelector string) (Result, error) {
	if dkimSelector == "" {
		dkimSelector = "default"
	}
	domain = strings.TrimSpace(strings.ToLower(domain))

	spfValid := false
	txt, err := c.resolver.LookupTXT(ctx, domain)
	if err != nil {
		if dnsErr, ok := err.(*net.DNSError); !ok || !dnsErr.IsNotFound {
			return Result{}, err
		}
	} else {
		for _, v := range txt {
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(v)), "v=spf1") {
				spfValid = true
				break
			}
		}
	}

	dkimValid := false
	dkimHost := dkimSelector + "._domainkey." + domain
	dkimTXT, err := c.resolver.LookupTXT(ctx, dkimHost)
	if err != nil {
		if dnsErr, ok := err.(*net.DNSError); !ok || !dnsErr.IsNotFound {
			return Result{}, err
		}
	} else {
		for _, v := range dkimTXT {
			if strings.Contains(strings.ToLower(v), "v=dkim1") {
				dkimValid = true
				break
			}
		}
	}

	dmarcValid := false
	dmarcHost := "_dmarc." + domain
	dmarcTXT, err := c.resolver.LookupTXT(ctx, dmarcHost)
	if err != nil {
		if dnsErr, ok := err.(*net.DNSError); !ok || !dnsErr.IsNotFound {
			return Result{}, err
		}
	} else {
		for _, v := range dmarcTXT {
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(v)), "v=dmarc1") {
				dmarcValid = true
				break
			}
		}
	}

	return Result{
		Domain:       domain,
		DKIMSelector: dkimSelector,
		SPFValid:     spfValid,
		DKIMValid:    dkimValid,
		DMARCValid:   dmarcValid,
		Ready:        spfValid && dkimValid && dmarcValid,
	}, nil
}
