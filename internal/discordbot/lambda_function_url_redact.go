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
	"authorization":         {},
	"cookie":                {},
	"proxy-authorization":   {},
}

func isSensitiveHTTPHeaderName(name string) bool {
	key := strings.ToLower(strings.TrimSpace(name))
	_, ok := sensitiveHTTPHeaderKeys[key]
	return ok
}

// redactHTTPHeadersForLog returns a copy of headers with signature and auth-related entries removed.
func redactHTTPHeadersForLog(src map[string]string) map[string]string {
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

func redactQueryStringParamsForLog(src map[string]string) map[string]string {
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

// LambdaFunctionURLDebugAttrs builds slog attributes for a Lambda Function URL request
// with signature, auth, and cookie values omitted (safe for DEBUG logs).
func LambdaFunctionURLDebugAttrs(req events.LambdaFunctionURLRequest, bodyLen int) []slog.Attr {
	rc := req.RequestContext
	httpDesc := rc.HTTP
	return []slog.Attr{
		slog.String("version", req.Version),
		slog.String("rawPath", req.RawPath),
		slog.String("rawQueryString", req.RawQueryString),
		slog.Bool("isBase64", req.IsBase64Encoded),
		slog.Int("bodyLen", bodyLen),
		slog.Bool("hasEd25519Signature", headerNonEmptyCI(req.Headers, "x-signature-ed25519")),
		slog.Bool("hasSignatureTimestamp", headerNonEmptyCI(req.Headers, "x-signature-timestamp")),
		slog.Any("headers", redactHTTPHeadersForLog(req.Headers)),
		slog.Any("queryParams", redactQueryStringParamsForLog(req.QueryStringParameters)),
		slog.Int("cookieCount", len(req.Cookies)),
		slog.String("requestId", rc.RequestID),
		slog.String("accountId", rc.AccountID),
		slog.String("domainName", rc.DomainName),
		slog.String("domainPrefix", rc.DomainPrefix),
		slog.Int64("timeEpoch", rc.TimeEpoch),
		slog.String("httpMethod", httpDesc.Method),
		slog.String("httpPath", httpDesc.Path),
		slog.String("httpProtocol", httpDesc.Protocol),
		slog.String("sourceIp", httpDesc.SourceIP),
		slog.String("userAgent", httpDesc.UserAgent),
	}
}
