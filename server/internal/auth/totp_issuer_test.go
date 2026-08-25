package auth

import (
	"net/url"
	"strings"
	"testing"
)

// The issuer is what an authenticator app shows beside the code, so it has to be the
// product's name. It used to be derived from the public URL's hostname, which meant every
// developer's phone said "localhost" and a deployment that moved renamed everyone's entry.
func TestEnrolmentCarriesTheIssuerIntoTheURI(t *testing.T) {
	enrolment, err := NewTOTPEnrollment("Kubby - MFA", "someone@example.com")
	if err != nil {
		t.Fatalf("enrol: %v", err)
	}

	parsed, err := url.Parse(enrolment.URI)
	if err != nil {
		t.Fatalf("the enrolment URI is not a URL: %v", err)
	}
	if parsed.Scheme != "otpauth" {
		t.Fatalf("scheme is %q, want otpauth", parsed.Scheme)
	}

	// Both places an authenticator reads it from. Apps differ over which they show, so a
	// label that disagrees with the parameter is an entry named differently on two phones.
	if issuer := parsed.Query().Get("issuer"); issuer != "Kubby - MFA" {
		t.Errorf("issuer parameter is %q", issuer)
	}
	label := strings.TrimPrefix(parsed.Path, "/")
	if !strings.HasPrefix(label, "Kubby - MFA:") {
		t.Errorf("label is %q, want it to start with the issuer", label)
	}
	if !strings.Contains(label, "someone@example.com") {
		t.Errorf("label %q does not name the account", label)
	}
}

// A colon separates the issuer from the account in the label, so one inside the issuer
// would split the name in the app.
func TestAnIssuerWithASpaceAndDashSurvivesEncoding(t *testing.T) {
	enrolment, err := NewTOTPEnrollment("Kubby - MFA", "a@example.com")
	if err != nil {
		t.Fatalf("enrol: %v", err)
	}

	parsed, _ := url.Parse(enrolment.URI)
	if got := parsed.Query().Get("issuer"); got != "Kubby - MFA" {
		t.Fatalf("the issuer did not survive encoding: %q", got)
	}
}

func TestEnrolmentProducesASecretAndAQRCode(t *testing.T) {
	enrolment, err := NewTOTPEnrollment("Kubby - MFA", "a@example.com")
	if err != nil {
		t.Fatalf("enrol: %v", err)
	}

	if enrolment.Secret == "" {
		t.Error("no secret was generated")
	}
	// Typing a 32-character secret by hand is error-prone, so the QR code is the primary
	// path rather than a nicety.
	if !strings.HasPrefix(enrolment.QRCodePNG, "data:image/png;base64,") {
		t.Errorf("the QR code is not an inline PNG: %.40s", enrolment.QRCodePNG)
	}
}
