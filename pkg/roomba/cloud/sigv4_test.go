package cloud

import (
	"net/http"
	"testing"
	"time"
)

// TestSignV4Vectors checks the signer against the AWS Signature Version 4 test
// suite (aws-sig-v4-test-suite: get-vanilla and get-vanilla-query-order-key-case),
// whose credentials and expected signatures are public test values.
func TestSignV4Vectors(t *testing.T) {
	creds := credentials{AccessKeyID: "AKIDEXAMPLE", SecretKey: "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY"}
	now := time.Date(2015, 8, 30, 12, 36, 0, 0, time.UTC)
	for _, tc := range []struct{ name, url, want string }{
		{"get-vanilla", "https://example.amazonaws.com/",
			"AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/20150830/us-east-1/service/aws4_request, SignedHeaders=host;x-amz-date, Signature=5fa00fa31553b73ebf1942676e86291e8372ff2a2260956d9b8aae1d763fbf31"},
		{"get-vanilla-query-order-key-case", "https://example.amazonaws.com/?Param2=value2&Param1=value1",
			"AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/20150830/us-east-1/service/aws4_request, SignedHeaders=host;x-amz-date, Signature=b97d918cfa904a5beff61c982a1b6f458b799221646efd99d3219ec94cdf2500"},
	} {
		req, err := http.NewRequest(http.MethodGet, tc.url, nil)
		if err != nil {
			t.Fatal(err)
		}
		signV4(req, nil, creds, "us-east-1", "service", now)
		if got := req.Header.Get("Authorization"); got != tc.want {
			t.Errorf("%s:\n got  %s\n want %s", tc.name, got, tc.want)
		}
		if got := req.Header.Get("X-Amz-Date"); got != "20150830T123600Z" {
			t.Errorf("%s: X-Amz-Date = %s", tc.name, got)
		}
	}
}

func TestSignV4SessionToken(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://example.amazonaws.com/v1/abc/pmaps?activeDetails=1", nil)
	signV4(req, nil, credentials{AccessKeyID: "AKID", SecretKey: "s", SessionToken: "tok"}, "eu-west-1", "execute-api", time.Now())
	if req.Header.Get("X-Amz-Security-Token") != "tok" {
		t.Fatal("session token header missing")
	}
}
