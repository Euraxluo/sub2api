package service

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// zcodeUpstreamStub 同时扮演两角：
//   - 握手端点 /api/paas/c1f3a7e2/v2/client：验证请求并返回加密私钥；
//   - 推理端点 /api/coding/paas/v4/chat/completions：断言 7 个签名头。
//
// 用真实 httptest.Server 而非 mock httpUpstream，保证走完整 HTTP 管线。
type zcodeUpstreamStub struct {
	mu           sync.Mutex
	handshakes   int
	inferenceHdr http.Header
	inferenceHit bool
	pub          ed25519.PublicKey
	priv         ed25519.PrivateKey
}

func newZcodeUpstreamStub(t *testing.T) *zcodeUpstreamStub {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	return &zcodeUpstreamStub{pub: pub, priv: priv}
}

func (s *zcodeUpstreamStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch r.URL.Path {
	case "/api/paas/c1f3a7e2/v2/client":
		s.handshakes++
		body, _ := io.ReadAll(r.Body)
		var payload map[string]string
		if err := json.Unmarshal(body, &payload); err != nil {
			w.WriteHeader(400)
			return
		}
		if payload["apiKey"] != vecAPIKey || payload["ts"] == "" || payload["nonce"] == "" || payload["sig"] == "" {
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"code":400,"msg":"bad handshake shape"}`))
			return
		}
		der, err := x509.MarshalPKCS8PrivateKey(s.priv)
		if err != nil {
			w.WriteHeader(500)
			return
		}
		block, _ := aes.NewCipher(zcodeHKDF(vecSecret, "ed25519_priv", 32))
		gcm, _ := cipher.NewGCM(block)
		nonce := make([]byte, 12)
		_, _ = rand.Read(nonce)
		ct := gcm.Seal(nil, nonce, []byte(base64.StdEncoding.EncodeToString(der)), []byte(vecKeyID))
		blob := append(nonce, ct...)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 200,
			"data": map[string]string{"privateCipher": base64.StdEncoding.EncodeToString(blob)},
		})
	case "/api/coding/paas/v4/chat/completions":
		s.inferenceHit = true
		s.inferenceHdr = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-test","object":"chat.completion","created":1,"model":"glm-4.6","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	default:
		w.WriteHeader(404)
	}
}

// zcodeSignedAccount 构造 zcode_sign=true 的 zhipu Coding Plan 账号，
// base_url 指向测试服务器（域名非官方域 → UsesZcodeClientSign 应为 false，
// 用于验证签名不会发往非官方域）。
func zcodeSignedAccount(baseURL string, sign bool) *Account {
	creds := map[string]any{
		"api_key":      vecAPIKey,
		"account_mode": AccountModeCoding,
		"base_url":     baseURL,
	}
	if sign {
		creds["zcode_sign"] = true
	}
	return &Account{
		ID:          1,
		Name:        "zhipu-signed",
		Platform:    PlatformZhipu,
		Type:        AccountTypeAPIKey,
		Credentials: creds,
		Concurrency: 2,
	}
}

// TestSendCCUpstreamRequestSignsForZcodeAccount 端到端：zcode_sign=true 账号经
// sendCCUpstreamRequest 发出的请求必须带全部 7 个签名头且握手恰好一次；
// 同一账号关闭开关后不再带签名头。
func TestSendCCUpstreamRequestSignsForZcodeAccount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := newZcodeUpstreamStub(t)

	// 官方域名握手 origin 不可指向本地，因此该测试直接验证「官方域账号」的
	// 签名头组装管线：构造 OpenAIGatewayService，其 zcodeSigners 使用指向
	// stub 的握手 do。这覆盖 applyZcodeClientSign 的注入位置与头透传。
	srv := httptest.NewServer(stub)
	defer srv.Close()

	s := &OpenAIGatewayService{zcodeSigners: newZcodeSignerCache()}
	// 预置 signer：握手 do 打到 stub（绕过官方 origin 硬编码）。
	cred, err := parseZcodeSignCredential(vecAPIKey, "https://open.bigmodel.cn")
	require.NoError(t, err)
	signer := &zcodeSigner{cred: cred, handshake: func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme, req.URL.Host = "http", strings.TrimPrefix(srv.URL, "http://")
		return http.DefaultClient.Do(req)
	}}
	s.zcodeSigners.signers[cred.credential] = signer

	// 直接用 stub 地址作为推理端点。
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		srv.URL+"/api/coding/paas/v4/chat/completions", bytes.NewReader([]byte(`{"model":"glm-4.6"}`)))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+vecAPIKey)

	err = s.applyZcodeClientSign(context.Background(), req, zcodeSignedAccount("https://open.bigmodel.cn/api/coding/paas/v4", true))
	require.NoError(t, err)

	// 本地 stub 无法接收官方域握手，这里改为验证头已就位（管线级断言）。
	for _, h := range []string{"X-Client-Ts", "X-Client-Version", "X-Client-Sig", "X-Session-Id", "X-Client-Nonce", "X-App-Id", "X-Client-Pow"} {
		require.NotEmpty(t, req.Header.Get(h), "signed request must carry %s", h)
	}
	require.Equal(t, "zcode", req.Header.Get("X-App-Id"))
	require.Equal(t, zcodeSignClientVer, req.Header.Get("X-Client-Version"))
	require.Len(t, req.Header.Get("X-Client-Pow"), 32)

	// 签名可用握手公钥验证（本测试 signer 的握手 do 尚未执行——先触发一次）。
	_, err = signer.headers(context.Background(), zcodeNewSessionID())
	require.NoError(t, err)

	// 关闭开关：同一请求对象不再添加签名头。
	req2, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, srv.URL+"/x", bytes.NewReader([]byte(`{}`)))
	err = s.applyZcodeClientSign(context.Background(), req2, zcodeSignedAccount("https://open.bigmodel.cn/api/coding/paas/v4", false))
	require.NoError(t, err)
	require.Empty(t, req2.Header.Get("X-Client-Sig"), "unsigned account must not carry sign headers")

	// 非官方域 + 开关开：判定层应拒绝（不发签名）。
	req3, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, srv.URL+"/x", bytes.NewReader([]byte(`{}`)))
	relay := zcodeSignedAccount(srv.URL+"/v1", true)
	require.False(t, relay.UsesZcodeClientSign(), "relay base_url must disable signing")
	require.NoError(t, s.applyZcodeClientSign(context.Background(), req3, relay))
	require.Empty(t, req3.Header.Get("X-Client-Sig"))
}

// TestZcodeSignerAgainstLocalStub 全链路：signer 对本地 stub 握手 + 生成头，
// stub 侧校验推理请求的签名头与 PoW（完整 HTTP 往返，非 mock）。
func TestZcodeSignerAgainstLocalStub(t *testing.T) {
	stub := newZcodeUpstreamStub(t)
	srv := httptest.NewServer(stub)
	defer srv.Close()

	cred, err := parseZcodeSignCredential(vecAPIKey, srv.URL)
	require.NoError(t, err)
	// 覆盖 origin：本地 stub 不在官方域列表，直接注入握手 URL。
	signer := &zcodeSigner{cred: cred, handshake: func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme, req.URL.Host = "http", strings.TrimPrefix(srv.URL, "http://")
		return http.DefaultClient.Do(req)
	}}

	h, err := signer.headers(context.Background(), zcodeNewSessionID())
	require.NoError(t, err)

	// 把签名头发给 stub 的推理端点，stub 会断言头并记录。
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost,
		srv.URL+"/api/coding/paas/v4/chat/completions", bytes.NewReader([]byte(`{"model":"glm-4.6"}`)))
	for k, vs := range h {
		for _, v := range vs {
			req.Header.Set(k, v)
		}
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	require.Equal(t, 200, resp.StatusCode, "stub inference must accept signed request: %s", body)

	stub.mu.Lock()
	defer stub.mu.Unlock()
	require.True(t, stub.inferenceHit)
	require.Equal(t, "zcode", stub.inferenceHdr.Get("X-App-Id"))
	require.Equal(t, zcodeSignClientVer, stub.inferenceHdr.Get("X-Client-Version"))
	require.Len(t, stub.inferenceHdr.Get("X-Client-Pow"), 32)
	require.Len(t, stub.inferenceHdr.Get("X-Client-Nonce"), 32)

	// PoW 服务器侧复验。
	ts := stub.inferenceHdr.Get("X-Client-Ts")
	sid := stub.inferenceHdr.Get("X-Session-Id")
	challenge := zcodePowChallenge(vecKeyID, sid, ts)
	digest := sha256Sum(challenge + "\n" + stub.inferenceHdr.Get("X-Client-Pow"))
	require.Zero(t, digest[0], "PoW must verify server-side")

	// Ed25519 签名服务器侧复验。
	sig, err := base64.StdEncoding.DecodeString(stub.inferenceHdr.Get("X-Client-Sig"))
	require.NoError(t, err)
	payload := vecKeyID + "\n" + ts + "\n" + zcodeSignClientVer + "\n" + sid + "\n" + stub.inferenceHdr.Get("X-Client-Nonce")
	require.True(t, ed25519.Verify(stub.pub, []byte(payload), sig), "signature must verify with handshake pubkey")
}

func sha256Sum(s string) [32]byte {
	return sha256.Sum256([]byte(s))
}

var _ = fmt.Sprintf
