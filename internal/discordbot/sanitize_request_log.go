package discordbot

import (
	"log/slog"
	"strings"

	"github.com/aws/aws-lambda-go/events"
)

// sensitiveHTTPHeaderKeys are lowercased; values must never appear in DEBUG logs.
var sensitiveHTTPHeaderKeys = map[string]struct{}{
	"x-signature-ed25519":   {},
	"x-signature-timestamp": {},
	"authorization":       {},
	"cookie":                {},
	"proxy-authorization":   {},
}

func isSensitiveHTTPHeaderName(name string) bool {
	key := strings.ToLower(strings.TrimSpace(name))
	_, ok := sensitiveHTTPHeaderKeys[key]
	return ok
}

// headersForLog returns a copy of headers with signature and auth-related entries removed.
func headersForLog(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]string, len(src))
	for k, v := range src {
		if isSensitiveHTTPHeaderName(k) {
			continue
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

var sensitiveQueryKeys = map[string]struct{}{
	"token":         {},
	"access_token":  {},
	"refresh_token": {},
	"code":          {},
	"client_secret": {},
}

func isSensitiveQueryKey(name string) bool {
	_, ok := sensitiveQueryKeys[strings.ToLower(strings.TrimSpace(name))]
	return ok
}

func queryStringParamsForLog(src map[string]string) map[string]string {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]string, len(src))
	for k, v := range src {
		if isSensitiveQueryKey(k) {
			continue
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// debugLogIncomingDiscordRequest logs Lambda Function URL request metadata suitable for
// troubleshooting Discord interactions without signature material, raw bodies, or cookies.
func debugLogIncomingDiscordRequest(req events.LambdaFunctionURLRequest, bodyLen int) {
	rc := req.RequestContext
	httpDesc := rc.HTTP

	attrs := []any{
		"version", req.Version,
		"rawPath", req.RawPath,
		"rawQueryString", req.RawQueryString,
		"isBase64", req.IsBase64Encoded,
		"bodyLen", bodyLen,
		"hasEd25519Signature", headerNonEmptyCI(req.Headers, "x-signature-ed25519"),
		"hasSignatureTimestamp", headerNonEmptyCI(req.Headers, "x-signature-timestamp"),
		"headers", headersForLog(req.Headers),
		"queryParams", queryStringParamsForLog(req.QueryStringParameters),
		"cookieCount", len(req.Cookies),
		"requestId", rc.RequestID,
		"accountId", rc.AccountID,
		"domainName", rc.DomainName,
		"domainPrefix", rc.DomainPrefix,
		"timeEpoch", rc.TimeEpoch,
		"httpMethod", httpDesc.Method,
		"httpPath", httpDesc.Path,
		"httpProtocol", httpDesc.Protocol,
		"sourceIp", httpDesc.SourceIP,
		"userAgent", httpDesc.UserAgent,
	}
	slog.Debug("discord request received", attrs...)
}

func headerNonEmptyCI(h map[string]string, nameLower string) bool {
	if len(h) == 0 {
		return false
	}
	for k, v := range h {
		if strings.EqualFold(strings.TrimSpace(k), nameLower) && strings.TrimSpace(v) != "" {
			return true
		}
	}
	return false
}
