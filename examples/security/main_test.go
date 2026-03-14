package main

import (
	"reflect"
	"testing"
)

func TestRunDemo(t *testing.T) {
	lines, err := runDemo()
	if err != nil {
		t.Fatalf("runDemo() error = %v", err)
	}

	want := []string{
		"policy baseline",
		"baseline trusted after login: active",
		"baseline trusted-user issue_session: trusted fast path",
		"baseline trusted after suspicious activity: challenged",
		"baseline trusted-user alert_security: session stepped up after suspicious activity",
		"baseline trusted-user prompt_mfa: suspicious activity",
		"baseline trusted after MFA pass: active",
		"baseline trusted-user issue_session: MFA passed",
		"baseline trusted final state: signed_out",
		"baseline trusted-user revoke_session: idle timeout",
		"baseline risky admin after login: challenged",
		"baseline risky-admin alert_security: geo risk login requires MFA",
		"baseline risky-admin prompt_mfa: geo risk",
		"baseline risky admin after MFA failure: locked",
		"baseline risky-admin lock_account: MFA failed",
		"baseline risky admin final state: signed_out",
		"baseline risky-admin clear_lockout",
		"",
		"policy strict",
		"strict trusted after login: challenged",
		"strict trusted-user alert_security: trusted device bypass disabled",
		"strict trusted-user prompt_mfa: strict policy requires MFA",
		"strict trusted after MFA pass: active",
		"strict trusted-user issue_session: MFA passed under strict policy",
		"strict trusted after suspicious activity: locked",
		"strict trusted-user alert_security: suspicious activity triggered immediate lock",
		"strict trusted-user lock_account: suspicious activity on strict policy",
		"strict trusted final state: signed_out",
		"strict trusted-user clear_lockout",
		"strict risky admin after login: challenged",
		"strict risky-admin alert_security: admin or geo-risk login requires MFA",
		"strict risky-admin prompt_mfa: high risk login",
		"strict risky admin after MFA failure: locked",
		"strict risky-admin lock_account: MFA failed",
		"strict risky admin final state: signed_out",
		"strict risky-admin clear_lockout",
	}

	if !reflect.DeepEqual(lines, want) {
		t.Fatalf("runDemo() = %#v, want %#v", lines, want)
	}
}
