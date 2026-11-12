package service

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

// 智谱客户端签名在 OpenAI 网关两条 zhipu 出站路径上的应用与 401 恢复：
//
//   - sendCCUpstreamRequest（/v1/chat/completions 与 adaptive 回退的 CC 直转）
//   - buildNativeAnthropicUpstreamRequest（anthropic 协议 /v1/messages 直通）
//
// 握手通过账号代理走 httpUpstream.Do（与推理请求同一出口，避免代理拓扑不一致
// 触发风控）。signer 按「凭证字符串」缓存在服务级 cache；401 VERIFY_* 时整体
// 重置，后续请求重新握手。fail-closed：签名失败直接返回错误，不发未签名请求。

// applyZcodeClientSign 为已启用客户端签名的 zhipu 账号给 req 打上签名头。
// 未启用或非 zhipu 账号是 no-op。返回错误即「本次请求应中止」（fail-closed）。
func (s *OpenAIGatewayService) applyZcodeClientSign(ctx context.Context, req *http.Request, account *Account) error {
	if !account.UsesZcodeClientSign() {
		return nil
	}
	h, err := s.zcodeSignedHeaders(ctx, account)
	if err != nil {
		return fmt.Errorf("zhipu client sign: %w", err)
	}
	for k, vs := range h {
		for _, v := range vs {
			req.Header.Set(k, v)
		}
	}
	return nil
}

// zcodeSignedHeaders 取 signer 并生成一组签名头（每请求新 session ID）。
func (s *OpenAIGatewayService) zcodeSignedHeaders(ctx context.Context, account *Account) (http.Header, error) {
	apiKey := strings.TrimSpace(account.GetOpenAIProtocolAPIKey())
	cred, err := parseZcodeSignCredential(apiKey, account.GetOpenAIBaseURL())
	if err != nil {
		return nil, err
	}
	do := func(req *http.Request) (*http.Response, error) {
		proxyURL := ""
		if account.ProxyID != nil && account.Proxy != nil {
			proxyURL = account.Proxy.URL()
		}
		return s.httpUpstream.Do(req, proxyURL, account.ID, maxInt(account.Concurrency, 1))
	}
	signer := s.zcodeSigners.get(cred, do)
	return signer.headers(ctx, zcodeNewSessionID())
}

// resetZcodeSigners 重置签名密钥缓存（401 VERIFY_SIGNATURE_INVALID /
// VERIFY_APIKEY_EXPIRED 时调用；官方客户端同场景重新握手重签一次）。
func (s *OpenAIGatewayService) resetZcodeSigners(account *Account) {
	if account == nil || !account.UsesZcodeClientSign() {
		return
	}
	s.zcodeSigners.resetAll()
}

// handleZcodeVerifyFailure 检查 401 响应体：命中 VERIFY_* 时重置 signer 并
// 记录 ops 事件。返回 true 表示命中（调用方继续走 failover 链路）。
func (s *OpenAIGatewayService) handleZcodeVerifyFailure(c *gin.Context, account *Account, respBody []byte) bool {
	if !isZcodeVerifyFailure(respBody) {
		return false
	}
	s.resetZcodeSigners(account)
	logger.L().Warn("zhipu client sign: verify failure, reset signing key",
		zap.Int64("account_id", account.ID),
		zap.String("account_name", account.Name),
	)
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		Platform:           account.Platform,
		AccountID:          account.ID,
		AccountName:        account.Name,
		UpstreamStatusCode: http.StatusUnauthorized,
		Kind:               "zcode_sign_verify_failure",
		Message:            "zhipu client sign verify failure (key reset, will re-handshake)",
	})
	return true
}
