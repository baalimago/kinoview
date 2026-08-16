package s3embed

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"
)

// signV4 signs req with AWS Signature Version 4 for the S3 service, in
// path-style addressing. body is the request payload; GET and empty-body PUT
// requests pass nil.
func signV4(req *http.Request, accessKey, secretKey, region, service string, body []byte) {
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")
	payloadHash := sha256Hex(body)

	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", payloadHash)

	canonicalRequest := strings.Join([]string{
		req.Method,
		canonicalURI(req),
		canonicalQuery(req),
		"host:" + req.Host + "\n" +
			"x-amz-content-sha256:" + payloadHash + "\n" +
			"x-amz-date:" + amzDate + "\n",
		"host;x-amz-content-sha256;x-amz-date",
		payloadHash,
	}, "\n")

	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		dateStamp + "/" + region + "/" + service + "/aws4_request",
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")

	signingKey := hmacSHA256(
		hmacSHA256(
			hmacSHA256(
				hmacSHA256([]byte("AWS4"+secretKey), []byte(dateStamp)),
				[]byte(region),
			),
			[]byte(service),
		),
		[]byte("aws4_request"),
	)
	signature := hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))

	req.Header.Set("Authorization",
		"AWS4-HMAC-SHA256 Credential="+accessKey+"/"+dateStamp+"/"+region+"/"+service+"/aws4_request, "+
			"SignedHeaders=host;x-amz-content-sha256;x-amz-date, Signature="+signature)
}

// canonicalURI returns the path-style S3 canonical URI: the escaped request
// path, or "/" when empty.
func canonicalURI(req *http.Request) string {
	p := req.URL.EscapedPath()
	if p == "" {
		return "/"
	}
	return p
}

// canonicalQuery returns the S3 canonical query string: parameters sorted by
// name, space-encoded as %20 (url.Values.Encode uses "+").
func canonicalQuery(req *http.Request) string {
	vals := req.URL.Query()
	if len(vals) == 0 {
		return ""
	}
	return strings.ReplaceAll(vals.Encode(), "+", "%20")
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}
