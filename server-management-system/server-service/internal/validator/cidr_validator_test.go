package validator

import (
	"errors"
	"testing"
)

func TestNewCIDRValidator_InvalidEntry(t *testing.T) {
	if _, err := NewCIDRValidator("10.0.0.0/8,not-a-cidr"); err == nil {
		t.Fatal("expected an error for a malformed CIDR entry, got nil")
	}
}

func TestNewCIDRValidator_IgnoresBlankEntries(t *testing.T) {
	v, err := NewCIDRValidator(" 10.0.0.0/8 , ,192.168.0.0/16, ")
	if err != nil {
		t.Fatalf("NewCIDRValidator failed: %v", err)
	}
	if err := v.Validate("10.1.2.3"); err != nil {
		t.Errorf("expected 10.1.2.3 to be allowed, got %v", err)
	}
	if err := v.Validate("192.168.1.1"); err != nil {
		t.Errorf("expected 192.168.1.1 to be allowed, got %v", err)
	}
}

func TestValidate_EmptyAllowlistDeniesAll(t *testing.T) {
	v, err := NewCIDRValidator("")
	if err != nil {
		t.Fatalf("NewCIDRValidator failed: %v", err)
	}
	for _, ip := range []string{"10.0.0.1", "192.168.1.1", "8.8.8.8"} {
		if err := v.Validate(ip); !errors.Is(err, ErrIPNotAllowed) {
			t.Errorf("Validate(%q) = %v, want ErrIPNotAllowed", ip, err)
		}
	}
}

func TestValidate_AllowsAddressInAllowlist(t *testing.T) {
	v, _ := NewCIDRValidator("10.0.0.0/8")
	if err := v.Validate("10.255.255.254"); err != nil {
		t.Errorf("expected 10.255.255.254 to be allowed, got %v", err)
	}
}

func TestValidate_RejectsAddressOutsideAllowlist(t *testing.T) {
	v, _ := NewCIDRValidator("10.0.0.0/8")
	if err := v.Validate("192.168.1.1"); !errors.Is(err, ErrIPNotAllowed) {
		t.Errorf("Validate(192.168.1.1) = %v, want ErrIPNotAllowed", err)
	}
}

func TestValidate_RejectsDeniedRanges(t *testing.T) {
	// 0.0.0.0/0 allows all, so rejections here come from the deny list.
	v, _ := NewCIDRValidator("0.0.0.0/0")

	cases := map[string]string{
		"unspecified":          "0.0.0.0",
		"loopback":             "127.0.0.1",
		"loopback high":        "127.255.255.255",
		"link-local":           "169.254.1.1",
		"cloud metadata":       "169.254.169.254",
		"multicast":            "224.0.0.1",
		"multicast high":       "239.255.255.255",
		"link-local multicast": "224.0.0.251",
	}
	for name, ip := range cases {
		if err := v.Validate(ip); !errors.Is(err, ErrIPNotAllowed) {
			t.Errorf("%s: Validate(%q) = %v, want ErrIPNotAllowed", name, ip, err)
		}
	}
}

func TestValidate_RejectsMalformedAndNonIPv4(t *testing.T) {
	v, _ := NewCIDRValidator("0.0.0.0/0")

	cases := map[string]string{
		"empty":        "",
		"garbage":      "not-an-ip",
		"truncated":    "192.168.1",
		"out of range": "999.1.1.1",
		"ipv6":         "2001:db8::1",
	}
	for name, ip := range cases {
		if err := v.Validate(ip); !errors.Is(err, ErrIPNotAllowed) {
			t.Errorf("%s: Validate(%q) = %v, want ErrIPNotAllowed", name, ip, err)
		}
	}
}

func TestValidate_TrimsSurroundingSpace(t *testing.T) {
	v, _ := NewCIDRValidator("10.0.0.0/8")
	if err := v.Validate("  10.1.2.3  "); err != nil {
		t.Errorf("expected padded address to be allowed, got %v", err)
	}
}
