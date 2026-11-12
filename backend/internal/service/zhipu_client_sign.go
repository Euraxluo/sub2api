package service

import (
	"bytes"
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
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// 智谱 BigModel「客户端签名」通道（官方客户端 ZCode 3.15.3 的 zhipu-client-sign，
// 内部 ClientRequestSigningV4）。Coding Plan 请求带上这套头后按官方签约客户端计
// 签约额度（1.5x 配额池），未签名请求按普通第三方计费。
//
// 协议（与官方一致的三个阶段）：
//
//  1. 握手：POST {origin}/api/paas/c1f3a7e2/v2/client，body 携带
//     {apiKey|accessToken, ts, nonce, sig}。sig = HMAC-SHA256(HKDF(secret,
//     "getSignKey_hmac"), "get_sign_key\n{keyID}\n{ts}\n{nonce}") base64。
//     响应 data.privateCipher = AES-256-GCM(HKDF(secret,"ed25519_priv"),
//     base64(DER(PKCS8 Ed25519)), aad=keyID)。
//  2. 每请求头：X-Client-Ts / X-Client-Version / X-Client-Sig / X-Session-Id /
//     X-Client-Nonce / X-App-Id / X-Client-Pow。Sig 对
//     "{keyID}\n{ts}\n{version}\n{sessionID}\n{nonce}" 做 Ed25519 签名（base64）。
//  3. PoW：challenge = SHA256("{keyID}\n{appId}\n{sessionID}\n{ts}").hex[:32]，
//     candidate = 24位随机hex + 8位计数hex，SHA256(challenge+"\n"+candidate)
//     首字节为 0（8 个前导零比特）。
//
// 凭证两种形态（同官方 handshakeField 判定）：普通 API Key `<id>.<secret>` 走
// apiKey 字段；项目访问 token（HS256 JWT，claims.token_use=project_access 且
// claims.api_key 存在）走 accessToken 字段，此时 keyID 取 claims.api_key。
//
// 官方在 VERIFY_SIGNATURE_INVALID / VERIFY_APIKEY_EXPIRED 401 后重置密钥重签一次；
// 本网关命中这两个错误时重置 signer 缓存并交给常规 failover（下次请求重新握手）。
// 握手失败按 magpie 插件同款策略 fail-closed：不发未签名请求——未签名 Coding Plan
// 请求按普通额度计费，静默降级等于把签约额度丢掉。

const (
	zcodeSignAppID     = "zcode"
	zcodeSignClientVer = "3.15.3"
	zcodeSignKDFSalt   = "WD_CLIENT_SIGN_KDF_SALT"
	zcodeSignPath      = "/api/paas/c1f3a7e2/v2/client"
	zcodeSignTimeout   = 30 * time.Second
	// PoW：8 个前导零比特（官方 SUs=8），candidate = 24 hex 随机前缀 + 8 hex 计数。
	zcodeSignPowNonceHexLen = 24
	zcodeSignPowMax         = 1 << 24 // 期望 ~256 次哈希；上限保守 2^24
)

// zcodeSignCredential 是解析后的签名凭证。
type zcodeSignCredential struct {
	keyID          string
	secret         string // API key 形态为 secret 段；token 形态为整个 JWT
	credential     string // 原样放入握手 body 与 Authorization
	handshakeField string // "apiKey" | "accessToken"
	baseURL        string // 账号 base_url：决定握手 origin（bigmodel / z.ai）
}

// parseZcodeSignCredential 解析 <id>.<secret> API key 或项目访问 JWT。
func parseZcodeSignCredential(apiKey, baseURL string) (zcodeSignCredential, error) {
	value := strings.TrimSpace(apiKey)
	if value == "" {
		return zcodeSignCredential{}, errors.New("empty credential")
	}
	parts := strings.Split(value, ".")
	if len(parts) == 3 && parts[2] != "" {
		if c, ok := parseZcodeProjectAccessToken(parts); ok {
			c.baseURL = baseURL
			return c, nil
		}
	}
	if at := strings.Index(value, "."); at > 0 && at == strings.LastIndex(value, ".") {
		id, secret := value[:at], value[at+1:]
		if strings.TrimSpace(id) != "" && strings.TrimSpace(secret) != "" &&
			id == strings.TrimSpace(id) && secret == strings.TrimSpace(secret) {
			return zcodeSignCredential{
				keyID: id, secret: secret, credential: value,
				handshakeField: "apiKey", baseURL: baseURL,
			}, nil
		}
	}
	return zcodeSignCredential{}, errors.New("credential must be an API key `<id>.<secret>` or a project access token")
}

// parseZcodeProjectAccessToken 识别三段 JWT：HS256 + typ=JWT + sign_type=SIGN 头，
// claims.token_use=project_access 且 claims.api_key 非空。
func parseZcodeProjectAccessToken(parts []string) (zcodeSignCredential, bool) {
	dec := func(seg string) []byte {
		raw := strings.NewReplacer("-", "+", "_", "/").Replace(seg)
		if pad := len(raw) % 4; pad != 0 {
			raw += strings.Repeat("=", 4-pad)
		}
		out, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			return nil
		}
		return out
	}
	var header struct {
		Alg      string `json:"alg"`
		Typ      string `json:"typ"`
		SignType string `json:"sign_type"`
	}
	if hdr := dec(parts[0]); hdr == nil || json.Unmarshal(hdr, &header) != nil {
		return zcodeSignCredential{}, false
	}
	if header.Alg != "HS256" || header.Typ != "JWT" || header.SignType != "SIGN" {
		return zcodeSignCredential{}, false
	}
	var claims struct {
		TokenUse string `json:"token_use"`
		APIKey   string `json:"api_key"`
	}
	if cl := dec(parts[1]); cl == nil || json.Unmarshal(cl, &claims) != nil {
		return zcodeSignCredential{}, false
	}
	if claims.TokenUse != "project_access" || strings.TrimSpace(claims.APIKey) == "" {
		return zcodeSignCredential{}, false
	}
	token := strings.Join(parts, ".")
	return zcodeSignCredential{
		keyID: claims.APIKey, secret: token, credential: token,
		handshakeField: "accessToken",
	}, true
}

// zcodeHKDF 是 RFC 5869 HKDF（SHA-256，salt=WD_CLIENT_SIGN_KDF_SALT），与官方
// WebCrypto HKDF(salt, ikm, info) 一致。
func zcodeHKDF(secret, info string, length int) []byte {
	prk := hmac.New(sha256.New, []byte(zcodeSignKDFSalt))
	prk.Write([]byte(secret))
	prkOut := prk.Sum(nil)

	var out []byte
	var t []byte
	for i := byte(1); len(out) < length; i++ {
		h := hmac.New(sha256.New, prkOut)
		h.Write(t)
		h.Write([]byte(info))
		h.Write([]byte{i})
		t = h.Sum(nil)
		out = append(out, t...)
	}
	return out[:length]
}

// zcodeGCMOpen 解 AES-256-GCM blob（nonce 前置 12B，tag 后置 16B）。
func zcodeGCMOpen(key, blob, aad []byte) ([]byte, error) {
	if len(blob) <= 12+16 {
		return nil, errors.New("handshake cipher too short")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, blob[:12], blob[12:], aad)
}

// zcodePowChallenge 计算 PoW challenge（SHA256 hex 前 32 字符）。
func zcodePowChallenge(keyID, sessionID, ts string) string {
	h := sha256.Sum256([]byte(keyID + "\n" + zcodeSignAppID + "\n" + sessionID + "\n" + ts))
	return fmt.Sprintf("%x", h)[:32]
}

// zcodeMinePow 暴力解 PoW：candidate = 24 hex 随机前缀 + 8 hex 计数，
// SHA256(challenge + "\n" + candidate) 首字节为 0。
func zcodeMinePow(challenge string) (string, error) {
	prefix := make([]byte, zcodeSignPowNonceHexLen/2)
	if _, err := rand.Read(prefix); err != nil {
		return "", err
	}
	prefixHex := fmt.Sprintf("%x", prefix)
	base := challenge + "\n" + prefixHex
	for c := range zcodeSignPowMax {
		candidate := fmt.Sprintf("%s%08x", prefixHex, c)
		digest := sha256.Sum256([]byte(base + fmt.Sprintf("%08x", c)))
		if digest[0] == 0 {
			return candidate, nil
		}
	}
	return "", errors.New("zhipu client sign: proof of work exhausted")
}

// zcodeSignHeaders 把一次请求的签名头写入 h（7 个头，同官方）。
func zcodeSignHeaders(h http.Header, c zcodeSignCredential, key ed25519.PrivateKey, sessionID string) error {
	ts := fmt.Sprintf("%d", time.Now().UnixMilli())
	nonceRaw := make([]byte, 16)
	if _, err := rand.Read(nonceRaw); err != nil {
		return err
	}
	nonce := fmt.Sprintf("%x", nonceRaw)
	payload := c.keyID + "\n" + ts + "\n" + zcodeSignClientVer + "\n" + sessionID + "\n" + nonce
	sig := ed25519.Sign(key, []byte(payload))
	pow, err := zcodeMinePow(zcodePowChallenge(c.keyID, sessionID, ts))
	if err != nil {
		return err
	}
	h.Set("X-Client-Ts", ts)
	h.Set("X-Client-Version", zcodeSignClientVer)
	h.Set("X-Client-Sig", base64.StdEncoding.EncodeToString(sig))
	h.Set("X-Session-Id", sessionID)
	h.Set("X-Client-Nonce", nonce)
	h.Set("X-App-Id", zcodeSignAppID)
	h.Set("X-Client-Pow", pow)
	return nil
}

// ── Signer：每凭证一把握手密钥，内存缓存 ───────────────────────────────

// zcodeHandshakeDo 由服务注入的握手 HTTP 发送抽象（生产 = httpUpstream.Do +
// 账号代理；测试可替换为 httptest）。
type zcodeHandshakeDo func(req *http.Request) (*http.Response, error)

// zcodeSigner 管理单个凭证的签名密钥生命周期。握手在互斥锁内执行：首个请求
// 支付握手时延，并发请求等待同一结果；失败不缓存，下次请求重试握手。
type zcodeSigner struct {
	cred      zcodeSignCredential
	handshake zcodeHandshakeDo

	mu  sync.Mutex
	key ed25519.PrivateKey
}

// headers 返回该凭证的签名头；key 未就绪时先握手。
func (s *zcodeSigner) headers(ctx context.Context, sessionID string) (http.Header, error) {
	key, err := s.ensureKey(ctx)
	if err != nil {
		return nil, err
	}
	h := http.Header{}
	if err := zcodeSignHeaders(h, s.cred, key, sessionID); err != nil {
		return nil, err
	}
	return h, nil
}

func (s *zcodeSigner) ensureKey(ctx context.Context) (ed25519.PrivateKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.key != nil {
		return s.key, nil
	}
	key, err := zcodeHandshake(ctx, s.handshake, s.cred)
	if err != nil {
		return nil, err
	}
	s.key = key
	return key, nil
}

// zcodeSignerCache 按凭证字符串缓存 signer。resetAll 丢弃全部缓存
// （401 VERIFY_* 后调用），下次请求重新握手。
type zcodeSignerCache struct {
	mu      sync.Mutex
	signers map[string]*zcodeSigner
}

func newZcodeSignerCache() *zcodeSignerCache {
	return &zcodeSignerCache{signers: make(map[string]*zcodeSigner)}
}

func (sc *zcodeSignerCache) get(cred zcodeSignCredential, do zcodeHandshakeDo) *zcodeSigner {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if s, ok := sc.signers[cred.credential]; ok {
		return s
	}
	s := &zcodeSigner{cred: cred, handshake: do}
	sc.signers[cred.credential] = s
	return s
}

func (sc *zcodeSignerCache) resetAll() {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.signers = make(map[string]*zcodeSigner)
}

// zcodeHandshakeOrigin 凭证对应的握手 origin（同推理域主机；账号层已限定官方域）。
func zcodeHandshakeOrigin(baseURL string) string {
	if strings.Contains(strings.ToLower(baseURL), "z.ai") {
		return "https://api.z.ai"
	}
	return "https://open.bigmodel.cn"
}

// zcodeHandshake 执行一次握手并解出 Ed25519 私钥。
func zcodeHandshake(ctx context.Context, do zcodeHandshakeDo, cred zcodeSignCredential) (ed25519.PrivateKey, error) {
	origin := zcodeHandshakeOrigin(cred.baseURL)
	ts := fmt.Sprintf("%d", time.Now().UnixMilli())
	nonceRaw := make([]byte, 16)
	if _, err := rand.Read(nonceRaw); err != nil {
		return nil, err
	}
	nonce := fmt.Sprintf("%x", nonceRaw)
	mac := hmac.New(sha256.New, zcodeHKDF(cred.secret, "getSignKey_hmac", 32))
	mac.Write([]byte("get_sign_key\n" + cred.keyID + "\n" + ts + "\n" + nonce))

	body, err := json.Marshal(map[string]string{
		cred.handshakeField: cred.credential,
		"ts":                ts,
		"nonce":             nonce,
		"sig":               base64.StdEncoding.EncodeToString(mac.Sum(nil)),
	})
	if err != nil {
		return nil, err
	}
	callCtx, cancel := context.WithTimeout(ctx, zcodeSignTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(callCtx, http.MethodPost, origin+zcodeSignPath, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", cred.credential)

	resp, err := do(req)
	if err != nil {
		return nil, fmt.Errorf("handshake transport: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, fmt.Errorf("handshake read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("handshake HTTP %d", resp.StatusCode)
	}
	var env struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			PrivateCipher string `json:"privateCipher"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &env); err != nil {
		return nil, fmt.Errorf("handshake decode: %w", err)
	}
	if env.Code != 200 || env.Data.PrivateCipher == "" {
		return nil, fmt.Errorf("handshake code %d msg %q", env.Code, env.Msg)
	}
	cipherBlob, err := base64.StdEncoding.DecodeString(env.Data.PrivateCipher)
	if err != nil {
		return nil, fmt.Errorf("handshake privateCipher base64: %w", err)
	}
	plain, err := zcodeGCMOpen(zcodeHKDF(cred.secret, "ed25519_priv", 32), cipherBlob, []byte(cred.keyID))
	if err != nil {
		return nil, fmt.Errorf("handshake decrypt: %w", err)
	}
	der, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(plain)))
	if err != nil {
		return nil, fmt.Errorf("handshake DER base64: %w", err)
	}
	return zcodePrivateKeyFromDER(der)
}

// zcodePrivateKeyFromDER 解析 PKCS8 Ed25519 私钥并做类型校验。
func zcodePrivateKeyFromDER(der []byte) (ed25519.PrivateKey, error) {
	parsed, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, fmt.Errorf("zhipu client sign key parse: %w", err)
	}
	key, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("zhipu client sign key is not Ed25519")
	}
	return key, nil
}

// zcodeNewSessionID 生成随机 32 hex 会话 ID（官方 nonce 16 字节）。
func zcodeNewSessionID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}

// isZcodeVerifyFailure 判定 401 响应体是否为签名校验失败（需重置握手密钥）。
func isZcodeVerifyFailure(body []byte) bool {
	return bytes.Contains(body, []byte("VERIFY_SIGNATURE_INVALID")) ||
		bytes.Contains(body, []byte("VERIFY_APIKEY_EXPIRED"))
}
