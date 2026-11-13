package app

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/fakeyanss/jt808-server-go/internal/config"
)

type Spec struct {
	// Binary 是二进制名，用于配置不匹配时的错误提示。
	Binary string
	// Mode 是固定的 server.mode；standalone 或 cluster。
	Mode string
	// RequiredRoles 是必须出现的角色。
	RequiredRoles []string
	// OptionalRoles 是允许额外出现的角色；固定角色二进制默认留空。
	OptionalRoles []string
}

// validate 校验配置文件与二进制的固定角色是否一致。
// 不匹配时错误信息直接给出应该使用的二进制。
func (s Spec) validate(options config.ClusterOptions) error {
	if s.Mode == "" {
		return nil
	}
	expected := s.Mode
	if len(s.RequiredRoles) > 0 {
		roles := append(append([]string{}, s.RequiredRoles...), s.OptionalRoles...)
		expected += " roles=" + strings.Join(roles, ",")
	}
	if options.Mode != s.Mode {
		return fmt.Errorf(
			"%s requires server.mode=%s (%s); current config has mode=%s roles=[%s]. use %s instead",
			s.Binary, s.Mode, expected, options.Mode,
			strings.Join(options.Roles.List(), ","), suggestBinary(options.Roles))
	}
	for _, role := range s.RequiredRoles {
		if !options.Roles.Has(role) {
			return fmt.Errorf(
				"%s requires cluster.roles to contain %q; current roles=[%s]. use %s instead",
				s.Binary, role, strings.Join(options.Roles.List(), ","), suggestBinary(options.Roles))
		}
	}
	allowed := map[string]bool{}
	for _, role := range s.RequiredRoles {
		allowed[role] = true
	}
	for _, role := range s.OptionalRoles {
		allowed[role] = true
	}
	for _, role := range options.Roles.List() {
		if !allowed[role] {
			return fmt.Errorf(
				"%s does not run role %q; current roles=[%s]. use %s instead",
				s.Binary, role, strings.Join(options.Roles.List(), ","), suggestBinary(options.Roles))
		}
	}
	return nil
}

// suggestBinary 根据配置里的角色组合给出应该使用的二进制。
func suggestBinary(roles config.RoleSet) string {
	switch {
	case len(roles) == 0:
		return "jt-server-standalone"
	case roles.Has(config.RoleGateway) && !roles.Has(config.RoleControl) &&
		!roles.Has(config.RoleMedia) && !roles.Has(config.RoleAttachment):
		return "jt-signal"
	case roles.Has(config.RoleControl) && !roles.Has(config.RoleGateway) && !roles.Has(config.RoleMedia):
		return "jt-control"
	case roles.Has(config.RoleMedia) && !roles.Has(config.RoleGateway) &&
		!roles.Has(config.RoleControl) && !roles.Has(config.RoleAttachment):
		return "jt-media"
	default:
		return "jt808-server (generic entry)"
	}
}

// checkResolvableURL 校验 URL 的 host 可以被解析。
func checkResolvableURL(name, raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return fmt.Errorf("%s is not a valid URL: %q", name, raw)
	}
	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("%s has no host: %q", name, raw)
	}
	if _, err := net.LookupHost(host); err != nil {
		return fmt.Errorf("%s host %q is not resolvable: %w", name, host, err)
	}
	return nil
}
