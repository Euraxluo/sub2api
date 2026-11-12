package service

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 跨实现向量：由 magpie zcode 插件同源 JS（/tmp/zcode-vectors.json 生成脚本）
// 产出，锁定 HKDF / 握手 MAC / PoW challenge / Ed25519 签名与官方实现一致。
const (
	vecAPIKey       = "vectorsKeyID.vectorsSecret"
	vecKeyID        = "vectorsKeyID"
	vecSecret       = "vectorsSecret"
	vecHKDFMac      = "02ff35f6c972711b2f3a9bef9ac5cd41126a1f7218825dbcebeeda188cccf591"
	vecHKDFPriv     = "3a84acfa57d51151277c89b24383b11828a7a5ce98a387af9c28d83e6d610d07"
	vecTS           = "1731312000000"
	vecNonce        = "00112233445566778899aabbccddeeff"
	vecHandshakeMac = "vbrdINp2Ja5Skto9EBfInAUiZu3KKxQKaa9x6so5gK4="
	vecSessionID    = "0123456789abcdef0123456789abcdef"
	vecChallenge    = "7e98426a78300f54ed6f625b7096c6b3"
	vecPowPrefix    = "a1b2c3d4e5f60718293a4b5c"
	vecPowCandidate = "a1b2c3d4e5f60718293a4b5c00000090"
	vecDERB64       = "MC4CAQAwBQYDK2VwBCIEIAqHexdjbMUcRcew7ds9tz3DtysC1cUUKEcuPZGu0op3"
	vecSigPayload   = "vectorsKeyID\n1731312000000\n3.15.3\n0123456789abcdef0123456789abcdef\n00112233445566778899aabbccddeeff"
	vecSig          = "8unnbjUgzVaHrtOoBfLNzUqNUbvsPo50/xlbQTOjDshi90eNWVsLIH4UU8Okfr6zz9SCudzSFOcU+TOinX2dCA=="
)

func mustHexDecode(t *testing.T, s string) []byte {
	t.Helper()
	out := make([]byte, len(s)/2)
	for i := range out {
		_, err := fmt.Sscanf(s[2*i:2*i+2], "%02x", &out[i])
		require.NoError(t, err)
	}
	return out
}

func TestZcodeHKDFVectors(t *testing.T) {
	require.Equal(t, vecHKDFMac, fmt.Sprintf("%x", zcodeHKDF(vecSecret, "getSignKey_hmac", 32)))
	require.Equal(t, vecHKDFPriv, fmt.Sprintf("%x", zcodeHKDF(vecSecret, "ed25519_priv", 32)))
}

func TestZcodeHandshakeMACVector(t *testing.T) {
	mac := hmac.New(sha256.New, mustHexDecode(t, vecHKDFMac))
	mac.Write([]byte("get_sign_key\n" + vecKeyID + "\n" + vecTS + "\n" + vecNonce))
	require.Equal(t, vecHandshakeMac, base64.StdEncoding.EncodeToString(mac.Sum(nil)))
}

func TestZcodePowChallengeVector(t *testing.T) {
	require.Equal(t, vecChallenge, zcodePowChallenge(vecKeyID, vecSessionID, vecTS))
}

func TestZcodePowCandidateVerifies(t *testing.T) {
	digest := sha256.Sum256([]byte(vecChallenge + "\n" + vecPowCandidate))
	require.Zero(t, digest[0], "vector PoW candidate must satisfy 8 leading zero bits")
	require.Len(t, vecPowCandidate, 32)
	require.True(t, strings.HasPrefix(vecPowCandidate, vecPowPrefix))
}

func TestZcodeEd25519SignVector(t *testing.T) {
	der, err := base64.StdEncoding.DecodeString(vecDERB64)
	require.NoError(t, err)
	key, err := zcodePrivateKeyFromDER(der)
	require.NoError(t, err)
	sig := ed25519.Sign(key, []byte(vecSigPayload))
	require.Equal(t, vecSig, base64.StdEncoding.EncodeToString(sig))
}

// gcmSealForTest 与官方 JS gcmSeal 同构（随机 nonce 前置）。
func gcmSealForTest(t *testing.T, key, plain, aad []byte) []byte {
	t.Helper()
	nonce := make([]byte, 12)
	_, err := rand.Read(nonce)
	require.NoError(t, err)
	block, err := aes.NewCipher(key)
	require.NoError(t, err)
	gcm, err := cipher.NewGCM(block)
	require.NoError(t, err)
	ct := gcm.Seal(nil, nonce, plain, aad)
	return append(nonce, ct...)
}

func TestZcodeGCMOpenRoundTrip(t *testing.T) {
	key := mustHexDecode(t, vecHKDFPriv)
	blob := gcmSealForTest(t, key, []byte(vecDERB64), []byte(vecKeyID))
	plain, err := zcodeGCMOpen(key, blob, []byte(vecKeyID))
	require.NoError(t, err)
	require.Equal(t, vecDERB64, string(plain))
}

func TestZcodeGCMOpenRejectsShortBlob(t *testing.T) {
	_, err := zcodeGCMOpen(make([]byte, 32), []byte("short"), []byte("aad"))
	require.Error(t, err)
}

func TestParseZcodeSignCredential(t *testing.T) {
	c, err := parseZcodeSignCredential("myid.mysecret", "https://open.bigmodel.cn/api/coding/paas/v4")
	require.NoError(t, err)
	require.Equal(t, "myid", c.keyID)
	require.Equal(t, "mysecret", c.secret)
	require.Equal(t, "apiKey", c.handshakeField)

	// token 形态：HS256 + typ=JWT + sign_type=SIGN，token_use=project_access。
	header, _ := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT", "sign_type": "SIGN"})
	claims, _ := json.Marshal(map[string]string{"token_use": "project_access", "api_key": "projKeyID"})
	enc := func(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
	token := enc(header) + "." + enc(claims) + ".c2ln"
	c, err = parseZcodeSignCredential(token, "https://api.z.ai/api/coding/paas/v4")
	require.NoError(t, err)
	require.Equal(t, "projKeyID", c.keyID)
	require.Equal(t, token, c.secret)
	require.Equal(t, "accessToken", c.handshakeField)
	require.Equal(t, "https://api.z.ai", zcodeHandshakeOrigin(c.baseURL))

	// 无效形态。
	for _, bad := range []string{"", "nodot", "a.b.c-not-jwt", ".secret", "id."} {
		_, err := parseZcodeSignCredential(bad, "")
		require.Error(t, err, "credential %q must be rejected", bad)
	}
}

// fakeHandshake 构造一个最小握手服务器：校验请求形状，返回加密私钥。
func fakeHandshake(t *testing.T, pub ed25519.PublicKey, priv ed25519.PrivateKey) zcodeHandshakeDo {
	return func(req *http.Request) (*http.Response, error) {
		require.Equal(t, "POST", req.Method)
		require.Equal(t, "/api/paas/c1f3a7e2/v2/client", req.URL.Path)
		require.Equal(t, vecAPIKey, req.Header.Get("Authorization"))
		body, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		var payload map[string]string
		require.NoError(t, json.Unmarshal(body, &payload))
		require.Equal(t, vecAPIKey, payload["apiKey"])
		require.NotEmpty(t, payload["ts"])
		require.NotEmpty(t, payload["nonce"])
		require.NotEmpty(t, payload["sig"])
		// 校验 sig 与官方公式一致。
		mac := hmac.New(sha256.New, zcodeHKDF(vecSecret, "getSignKey_hmac", 32))
		mac.Write([]byte("get_sign_key\n" + vecKeyID + "\n" + payload["ts"] + "\n" + payload["nonce"]))
		require.Equal(t, base64.StdEncoding.EncodeToString(mac.Sum(nil)), payload["sig"])
		// 加密 PKCS8 DER（base64 文本）作为 privateCipher。
		der, err := x509.MarshalPKCS8PrivateKey(priv)
		require.NoError(t, err)
		blob := gcmSealForTest(t, zcodeHKDF(vecSecret, "ed25519_priv", 32),
			[]byte(base64.StdEncoding.EncodeToString(der)), []byte(vecKeyID))
		env := map[string]any{
			"code": 200,
			"data": map[string]string{"privateCipher": base64.StdEncoding.EncodeToString(blob)},
		}
		out, _ := json.Marshal(env)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(string(out))),
		}, nil
	}
}

func TestZcodeSignerHeadersEndToEnd(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	_ = pub

	cred, err := parseZcodeSignCredential(vecAPIKey, "https://open.bigmodel.cn/api/coding/paas/v4")
	require.NoError(t, err)
	signer := &zcodeSigner{cred: cred, handshake: fakeHandshake(t, pub, priv)}

	h, err := signer.headers(context.Background(), zcodeNewSessionID())
	require.NoError(t, err)
	require.Equal(t, "zcode", h.Get("X-App-Id"))
	require.Equal(t, "3.15.3", h.Get("X-Client-Version"))
	require.Len(t, h.Get("X-Client-Nonce"), 32)
	require.Len(t, h.Get("X-Session-Id"), 32)
	require.Len(t, h.Get("X-Client-Pow"), 32)
	ts := h.Get("X-Client-Ts")
	require.NotEmpty(t, ts)

	// PoW 头自验证：challenge 重算 + 首字节为 0。
	challenge := zcodePowChallenge(vecKeyID, h.Get("X-Session-Id"), ts)
	digest := sha256.Sum256([]byte(challenge + "\n" + h.Get("X-Client-Pow")))
	require.Zero(t, digest[0])

	// 签名头验证（用握手返回的公钥）。
	sig, err := base64.StdEncoding.DecodeString(h.Get("X-Client-Sig"))
	require.NoError(t, err)
	payload := vecKeyID + "\n" + ts + "\n" + zcodeSignClientVer + "\n" + h.Get("X-Session-Id") + "\n" + h.Get("X-Client-Nonce")
	require.True(t, ed25519.Verify(pub, []byte(payload), sig))

	// 第二次调用复用缓存密钥：握手 do 不再被调用。
	called := false
	signer.handshake = func(req *http.Request) (*http.Response, error) {
		called = true
		return nil, fmt.Errorf("should not be called")
	}
	_, err = signer.headers(context.Background(), zcodeNewSessionID())
	require.NoError(t, err)
	require.False(t, called)
}

func TestZcodeSignerFailClosed(t *testing.T) {
	cred, err := parseZcodeSignCredential(vecAPIKey, "https://open.bigmodel.cn")
	require.NoError(t, err)
	signer := &zcodeSigner{cred: cred, handshake: func(req *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("network down")
	}}
	_, err = signer.headers(context.Background(), zcodeNewSessionID())
	require.Error(t, err, "handshake failure must fail the request (no unsigned fallback)")
}

func TestZcodeSignerCacheResetAll(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	_ = pub
	cred, _ := parseZcodeSignCredential(vecAPIKey, "https://open.bigmodel.cn")
	cache := newZcodeSignerCache()
	handshakes := 0
	do := func(req *http.Request) (*http.Response, error) {
		handshakes++
		return fakeHandshake(t, pub, priv)(req)
	}
	s1 := cache.get(cred, do)
	_, err := s1.headers(context.Background(), zcodeNewSessionID())
	require.NoError(t, err)
	require.Equal(t, 1, handshakes)

	// 同凭证复用 signer。
	s2 := cache.get(cred, do)
	require.Same(t, s1, s2)

	// reset 后新 signer，重新握手。
	cache.resetAll()
	s3 := cache.get(cred, do)
	require.NotSame(t, s1, s3)
	_, err = s3.headers(context.Background(), zcodeNewSessionID())
	require.NoError(t, err)
	require.Equal(t, 2, handshakes)
}

func TestZcodeSignerConcurrentHandshakeSingleFlight(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	cred, _ := parseZcodeSignCredential(vecAPIKey, "https://open.bigmodel.cn")
	var mu sync.Mutex
	handshakes := 0
	do := func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		handshakes++
		mu.Unlock()
		return fakeHandshake(t, pub, priv)(req)
	}
	signer := &zcodeSigner{cred: cred, handshake: do}

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := signer.headers(context.Background(), zcodeNewSessionID())
			require.NoError(t, err)
		}()
	}
	wg.Wait()
	require.Equal(t, 1, handshakes, "concurrent requests must share one handshake")
}

func TestZcodeVerifyFailureDetection(t *testing.T) {
	require.True(t, isZcodeVerifyFailure([]byte(`{"error":{"code":"VERIFY_SIGNATURE_INVALID"}}`)))
	require.True(t, isZcodeVerifyFailure([]byte(`VERIFY_APIKEY_EXPIRED`)))
	require.False(t, isZcodeVerifyFailure([]byte(`{"error":"invalid api key"}`)))
	require.False(t, isZcodeVerifyFailure(nil))
}

// handleZcodeVerifyFailure 需要 gin.Context（appendOpsUpstreamError 内部有 nil 守卫）。
func TestHandleZcodeVerifyFailureResetsCache(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &OpenAIGatewayService{zcodeSigners: newZcodeSignerCache()}
	cred, _ := parseZcodeSignCredential(vecAPIKey, "https://open.bigmodel.cn")
	_ = s.zcodeSigners.get(cred, nil)
	require.Len(t, s.zcodeSigners.signers, 1)

	account := &Account{Platform: PlatformZhipu, Credentials: map[string]any{
		"api_key":      vecAPIKey,
		"account_mode": AccountModeCoding,
		"zcode_sign":   true,
		"base_url":     "https://open.bigmodel.cn/api/coding/paas/v4",
	}}
	c, _ := gin.CreateTestContext(nil)
	require.True(t, s.handleZcodeVerifyFailure(c, account, []byte("VERIFY_SIGNATURE_INVALID")))
	require.Empty(t, s.zcodeSigners.signers, "cache must be reset")
	require.False(t, s.handleZcodeVerifyFailure(c, account, []byte("other error")))
}

// TestUsesZcodeClientSignGating 锁定启用判定：仅 zhipu + coding + zcode_sign=true
// + 官方域。
func TestUsesZcodeClientSignGating(t *testing.T) {
	base := func() *Account {
		return &Account{
			Platform: PlatformZhipu,
			Type:     AccountTypeAPIKey,
			Credentials: map[string]any{
				"api_key":      vecAPIKey,
				"account_mode": AccountModeCoding,
				"zcode_sign":   true,
				"base_url":     "https://open.bigmodel.cn/api/coding/paas/v4",
			},
		}
	}
	require.True(t, base().UsesZcodeClientSign())

	notZhipu := base()
	notZhipu.Platform = PlatformKimi
	require.False(t, notZhipu.UsesZcodeClientSign())

	payg := base()
	payg.Credentials["account_mode"] = AccountModePayG
	require.False(t, payg.UsesZcodeClientSign())

	noFlag := base()
	noFlag.Credentials["zcode_sign"] = false
	require.False(t, noFlag.UsesZcodeClientSign())

	missingFlag := base()
	delete(missingFlag.Credentials, "zcode_sign")
	require.False(t, missingFlag.UsesZcodeClientSign())

	relay := base()
	relay.Credentials["base_url"] = "https://my-relay.example.com/v1"
	require.False(t, relay.UsesZcodeClientSign())

	zai := base()
	zai.Credentials["base_url"] = "https://api.z.ai/api/coding/paas/v4"
	require.True(t, zai.UsesZcodeClientSign())

	boolFlag := base()
	boolFlag.Credentials["zcode_sign"] = true
	require.True(t, boolFlag.UsesZcodeClientSign(), "json bool true must be accepted")
}
