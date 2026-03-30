package service

import (
	"context"
	"strings"

	"github.com/srmdn/maild/internal/domaincheck"
)

type DomainStore interface {
	UpsertDomainVerification(ctx context.Context, workspaceID int64, domain string, verified bool) error
}

type DomainService struct {
	store   DomainStore
	checker *domaincheck.Checker
}

func NewDomainService(store DomainStore, checker *domaincheck.Checker) *DomainService {
	return &DomainService{store: store, checker: checker}
}

func (s *DomainService) CheckReadiness(ctx context.Context, workspaceID int64, domain, dkimSelector string) (domaincheck.Result, error) {
	result, err := s.checker.Check(ctx, strings.TrimSpace(domain), strings.TrimSpace(dkimSelector))
	if err != nil {
		return domaincheck.Result{}, err
	}
	if err := s.store.UpsertDomainVerification(ctx, workspaceID, result.Domain, result.Ready); err != nil {
		return domaincheck.Result{}, err
	}
	return result, nil
}
