package gateway

import (
	"os"
	"testing"
)

func TestAdminAuth_Lifecycle(t *testing.T) {
	tmpFile := t.TempDir() + "/gateway_config_auth_test.json"
	_ = os.Setenv("GATEWAY_CONFIG_FILE", tmpFile)
	defer os.Unsetenv("GATEWAY_CONFIG_FILE")

	// Initially not configured
	if IsAdminConfigured() {
		t.Fatalf("expected admin not configured initially")
	}

	// Setup password
	err := SetupAdminPassword("SecurePass123!")
	if err != nil {
		t.Fatalf("failed to setup admin password: %v", err)
	}

	if !IsAdminConfigured() {
		t.Fatalf("expected admin to be configured now")
	}

	// Duplicate setup should fail
	if err := SetupAdminPassword("AnotherPass"); err == nil {
		t.Fatalf("expected duplicate setup to fail")
	}

	// Verify incorrect password
	if VerifyAdminPassword("WrongPass") {
		t.Fatalf("expected wrong password to fail verification")
	}

	// Verify correct password
	if !VerifyAdminPassword("SecurePass123!") {
		t.Fatalf("expected correct password to succeed verification")
	}

	// Session management
	sessToken := CreateSession()
	if !ValidateSession(sessToken) {
		t.Fatalf("expected session token to be valid")
	}
	if ValidateSession("fake-token") {
		t.Fatalf("expected fake token to be invalid")
	}

	RevokeSession(sessToken)
	if ValidateSession(sessToken) {
		t.Fatalf("expected revoked session token to be invalid")
	}

	// Change password
	err = ChangeAdminPassword("WrongOldPass", "BrandNewPass456!")
	if err == nil {
		t.Fatalf("expected wrong old password to fail change")
	}

	err = ChangeAdminPassword("SecurePass123!", "BrandNewPass456!")
	if err != nil {
		t.Fatalf("expected change password to succeed: %v", err)
	}

	if !VerifyAdminPassword("BrandNewPass456!") {
		t.Fatalf("expected new password to work")
	}
}

