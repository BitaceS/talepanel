package services

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// settingRequireStaff2FA is the app_settings key of the staff 2FA policy.
const settingRequireStaff2FA = "security.require_staff_2fa"

// ErrOwnTwoFactorMissing: an owner cannot require 2FA from staff before
// enabling it on their own account (they would lock themselves out of
// everything but the setup page).
var ErrOwnTwoFactorMissing = errors.New("enable two-factor authentication on your own account first")

// SecurityPolicy is the panel-wide security policy an owner can change.
type SecurityPolicy struct {
	// RequireStaff2FA: admins and owners without 2FA can only reach their own
	// account (to set 2FA up) until they enable it.
	RequireStaff2FA bool `json:"require_staff_2fa"`
}

// SecurityPolicyService stores the security policy in app_settings. Reads go
// through a short cache: the auth middleware asks on every request.
type SecurityPolicyService struct {
	settings *AppSettingsService
	log      *zap.Logger
	ttl      time.Duration

	mu       sync.Mutex
	cached   SecurityPolicy
	cachedAt time.Time
}

func NewSecurityPolicyService(settings *AppSettingsService, log *zap.Logger) *SecurityPolicyService {
	return &SecurityPolicyService{settings: settings, log: log, ttl: 15 * time.Second}
}

// Get returns the current policy.
func (s *SecurityPolicyService) Get(ctx context.Context) (SecurityPolicy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.cachedAt.IsZero() && time.Since(s.cachedAt) < s.ttl {
		return s.cached, nil
	}
	v, err := s.settings.Get(ctx, settingRequireStaff2FA)
	if err != nil {
		return SecurityPolicy{}, err
	}
	s.cached = SecurityPolicy{RequireStaff2FA: v == "1"}
	s.cachedAt = time.Now()
	return s.cached, nil
}

// RequireStaff2FA is the auth middleware's view of the policy. If the setting
// cannot be read it keeps the last known value (off before the first read),
// so a database hiccup neither locks staff out nor silently drops a policy
// that was on.
func (s *SecurityPolicyService) RequireStaff2FA(ctx context.Context) bool {
	p, err := s.Get(ctx)
	if err != nil {
		s.log.Warn("reading the security policy failed; using the last known value", zap.Error(err))
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.cached.RequireStaff2FA
	}
	return p.RequireStaff2FA
}

// Set stores the policy. callerHas2FA is whether the owner making the change
// has 2FA enabled; requiring it from others without it is refused.
func (s *SecurityPolicyService) Set(ctx context.Context, p SecurityPolicy, callerID uuid.UUID, callerHas2FA bool) (SecurityPolicy, error) {
	if p.RequireStaff2FA && !callerHas2FA {
		return SecurityPolicy{}, ErrOwnTwoFactorMissing
	}
	v := ""
	if p.RequireStaff2FA {
		v = "1"
	}
	if err := s.settings.Set(ctx, settingRequireStaff2FA, v, &callerID); err != nil {
		return SecurityPolicy{}, err
	}
	s.mu.Lock()
	s.cached, s.cachedAt = p, time.Now()
	s.mu.Unlock()
	return p, nil
}
