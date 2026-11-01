package storage

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/tencentyun/cos-go-sdk-v5"
)

var _ MediaStore = (*TencentCOS)(nil)

// PresignPost 生成 COS POST Object 的表单上传凭证。
//
// 使用 COS 的 POST Object 签名流程（q-sign-* 字段）：
//
//	KeyTime    = start;end
//	SignKey    = hex(HMAC-SHA1(SecretKey, KeyTime))
//	SignToSign = hex(SHA1(PolicyJSON))
//	Signature  = hex(HMAC-SHA1(SignKey, SignToSign))
//
// policy 同时限制 key、Content-Type、大小范围与过期时间，
// COS 会在上传阶段拒绝超限/字段不符的请求。
func (s *TencentCOS) PresignPost(ctx context.Context, key, contentType string, maxBytes int64, ttl time.Duration) (string, map[string]string, time.Time, error) {
	if s.bucketName == "" {
		return "", nil, time.Time{}, fmt.Errorf("无法从 bucket URL 解析桶名，不能生成 POST policy")
	}
	key = strings.TrimLeft(strings.TrimSpace(key), "/")
	if key == "" {
		return "", nil, time.Time{}, fmt.Errorf("缺少 object key")
	}
	if strings.TrimSpace(contentType) == "" {
		return "", nil, time.Time{}, fmt.Errorf("缺少 Content-Type")
	}
	if maxBytes <= 0 {
		return "", nil, time.Time{}, fmt.Errorf("maxBytes 必须大于 0")
	}
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}

	now := time.Now()
	expiresAt := now.Add(ttl)
	// 起始时间回退 1 分钟，容忍客户端/服务端轻微的时钟差异。
	keyTime := fmt.Sprintf("%d;%d", now.Add(-time.Minute).Unix(), expiresAt.Unix())

	_, policyB64, signature := buildPostPolicy(
		s.bucketName, s.secretID, s.secretKey,
		key, contentType, maxBytes, keyTime, expiresAt,
	)

	fields := map[string]string{
		"key":                   key,
		"policy":                policyB64,
		"q-sign-algorithm":      "sha1",
		"q-ak":                  s.secretID,
		"q-key-time":            keyTime,
		"q-signature":           signature,
		"Content-Type":          contentType,
		"success_action_status": "200",
	}

	return strings.TrimRight(s.bucketURL, "/") + "/", fields, expiresAt, nil
}

// buildPostPolicy 构造 policy JSON、Base64 与签名。
// 独立成函数便于用固定时间做单元测试。
func buildPostPolicy(bucket, secretID, secretKey, key, contentType string, maxBytes int64, keyTime string, expiresAt time.Time) ([]byte, string, string) {
	policy := map[string]interface{}{
		"expiration": expiresAt.UTC().Format("2006-01-02T15:04:05.000Z"),
		"conditions": []interface{}{
			map[string]string{"bucket": bucket},
			[]interface{}{"content-length-range", 1, maxBytes},
			[]interface{}{"eq", "$key", key},
			[]interface{}{"eq", "$Content-Type", contentType},
			map[string]string{"success_action_status": "200"},
			map[string]string{"q-sign-algorithm": "sha1"},
			map[string]string{"q-ak": secretID},
			map[string]string{"q-sign-time": keyTime},
		},
	}

	policyJSON, _ := json.Marshal(policy)
	policyB64 := base64.StdEncoding.EncodeToString(policyJSON)

	signKey := hmacSHA1Hex(secretKey, keyTime)
	stringToSign := sha1Hex(policyJSON)
	signature := hmacSHA1Hex(signKey, stringToSign)

	return policyJSON, policyB64, signature
}

func hmacSHA1Hex(key, message string) string {
	mac := hmac.New(sha1.New, []byte(key))
	mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}

func sha1Hex(data []byte) string {
	sum := sha1.Sum(data)
	return hex.EncodeToString(sum[:])
}

// Head 查询对象元数据。
func (s *TencentCOS) Head(ctx context.Context, key string) (*ObjectMeta, error) {
	key = strings.TrimLeft(strings.TrimSpace(key), "/")
	if key == "" {
		return nil, fmt.Errorf("缺少 object key")
	}

	resp, err := s.client.Object.Head(ctx, key, nil)
	if err != nil {
		if cos.IsNotFoundError(err) {
			return nil, ErrObjectNotFound
		}
		return nil, fmt.Errorf("COS HEAD 失败: %w", err)
	}
	if resp == nil || resp.Response == nil {
		return nil, fmt.Errorf("COS HEAD 返回空响应")
	}
	if resp.Body != nil {
		defer resp.Body.Close()
	}

	size, _ := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64)
	return &ObjectMeta{
		Size:        size,
		ContentType: resp.Header.Get("Content-Type"),
		ETag:        strings.Trim(resp.Header.Get("ETag"), `"`),
	}, nil
}

// GetRange 读取对象的一段字节，用于魔数校验。
func (s *TencentCOS) GetRange(ctx context.Context, key string, start, end int64) ([]byte, error) {
	key = strings.TrimLeft(strings.TrimSpace(key), "/")
	if key == "" {
		return nil, fmt.Errorf("缺少 object key")
	}
	if start < 0 || end < start {
		return nil, fmt.Errorf("range 参数无效: %d-%d", start, end)
	}

	resp, err := s.client.Object.Get(ctx, key, &cos.ObjectGetOptions{
		Range: fmt.Sprintf("bytes=%d-%d", start, end),
	})
	if err != nil {
		return nil, fmt.Errorf("COS Range GET 失败: %w", err)
	}
	defer resp.Body.Close()

	return io.ReadAll(io.LimitReader(resp.Body, end-start+2))
}

// Copy 服务端复制对象。
func (s *TencentCOS) Copy(ctx context.Context, srcKey, dstKey string) error {
	srcKey = strings.TrimLeft(strings.TrimSpace(srcKey), "/")
	dstKey = strings.TrimLeft(strings.TrimSpace(dstKey), "/")
	if srcKey == "" || dstKey == "" {
		return fmt.Errorf("缺少源或目标 object key")
	}

	// SDK 需要 host/key 形式的 copy source（不带 scheme）。
	sourceURL := s.client.BaseURL.BucketURL.Host + "/" + srcKey
	if _, _, err := s.client.Object.Copy(ctx, dstKey, sourceURL, nil); err != nil {
		return fmt.Errorf("COS Copy 失败: %w", err)
	}
	return nil
}

// ProbeAVInfo 使用 CI 媒体信息接口解析音频/视频。
//
// 走 SDK 的 GetMediaInfo（COS 域名 ?ci-process=videoinfo），
// 返回 XML 已被 SDK 解析成 MediaInfo 结构，避免自己处理两种响应格式。
func (s *TencentCOS) ProbeAVInfo(ctx context.Context, key string) (*MediaProbe, error) {
	key = strings.TrimLeft(strings.TrimSpace(key), "/")
	if key == "" {
		return nil, fmt.Errorf("缺少 object key")
	}

	res, _, err := s.client.CI.GetMediaInfo(ctx, key, nil)
	if err != nil {
		return nil, fmt.Errorf("CI 媒体信息解析失败: %w", err)
	}
	if res == nil || res.MediaInfo == nil {
		return nil, fmt.Errorf("CI 媒体信息为空")
	}

	mi := res.MediaInfo
	probe := &MediaProbe{Format: strings.TrimSpace(mi.Format.FormatName)}

	if d, err := strconv.ParseFloat(strings.TrimSpace(mi.Format.Duration), 64); err == nil {
		probe.DurationMS = int64(math.Round(d * 1000))
	}

	for _, v := range mi.Stream.Video {
		if codec := strings.ToLower(strings.TrimSpace(v.CodecName)); codec != "" {
			probe.VideoCodecs = append(probe.VideoCodecs, codec)
		}
		if probe.Width == 0 {
			probe.Width, _ = strconv.Atoi(strings.TrimSpace(v.Width))
		}
		if probe.Height == 0 {
			probe.Height, _ = strconv.Atoi(strings.TrimSpace(v.Height))
		}
		if probe.FPS == 0 {
			probe.FPS = parseFPS(v.AvgFps)
			if probe.FPS == 0 {
				probe.FPS = parseFPS(v.Fps)
			}
		}
		if probe.DurationMS == 0 {
			if d, err := strconv.ParseFloat(strings.TrimSpace(v.Duration), 64); err == nil {
				probe.DurationMS = int64(math.Round(d * 1000))
			}
		}
	}

	for _, a := range mi.Stream.Audio {
		if codec := strings.ToLower(strings.TrimSpace(a.CodecName)); codec != "" {
			probe.AudioCodecs = append(probe.AudioCodecs, codec)
		}
		if probe.DurationMS == 0 {
			if d, err := strconv.ParseFloat(strings.TrimSpace(a.Duration), 64); err == nil {
				probe.DurationMS = int64(math.Round(d * 1000))
			}
		}
	}

	probe.HasVideo = len(mi.Stream.Video) > 0
	probe.HasAudio = len(mi.Stream.Audio) > 0
	return probe, nil
}

// parseFPS 解析 "24.000000" 或 "24000/1001" 形式的帧率。
func parseFPS(raw string) float64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	if i := strings.Index(raw, "/"); i > 0 {
		num, err1 := strconv.ParseFloat(raw[:i], 64)
		den, err2 := strconv.ParseFloat(raw[i+1:], 64)
		if err1 == nil && err2 == nil && den != 0 {
			return num / den
		}
		return 0
	}
	v, _ := strconv.ParseFloat(raw, 64)
	return v
}

// ProbeImageInfo 使用 CI 图片信息接口解析宽高。
//
// 请求是 COS 域名的 ?imageInfo，响应通常是 XML；这里同时兼容 JSON，
// 避免不同接口形态下解析失败。
func (s *TencentCOS) ProbeImageInfo(ctx context.Context, key string) (*ImageProbe, error) {
	key = strings.TrimLeft(strings.TrimSpace(key), "/")
	if key == "" {
		return nil, fmt.Errorf("缺少 object key")
	}

	resp, err := s.client.CI.Get(ctx, key, "imageInfo", nil)
	if err != nil {
		return nil, fmt.Errorf("CI 图片信息解析失败: %w", err)
	}
	if resp == nil || resp.Body == nil {
		return nil, fmt.Errorf("CI 图片信息返回空响应")
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("读取 CI 图片信息失败: %w", err)
	}

	return parseImageInfoBody(body)
}

// parseImageInfoBody 解析 CI imageInfo 响应。
//
// 实测返回是顶层 JSON，且 width/height 为字符串：
// {"format":"png","width":"300","height":"300",...}
// 同时兼容 XML 与包一层 imageInfo 的形态。
func parseImageInfoBody(body []byte) (*ImageProbe, error) {
	var xmlResp struct {
		ImageInfo cos.PicImageInfo `xml:"ImageInfo"`
	}
	if err := xml.Unmarshal(body, &xmlResp); err == nil && xmlResp.ImageInfo.Width > 0 {
		return &ImageProbe{
			Width:  xmlResp.ImageInfo.Width,
			Height: xmlResp.ImageInfo.Height,
			Format: normalizeImageFormat(xmlResp.ImageInfo.Format),
		}, nil
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err == nil {
		width := valueToInt(raw["width"])
		height := valueToInt(raw["height"])
		format := valueToString(raw["format"])
		if width == 0 {
			if nested, ok := raw["imageInfo"].(map[string]interface{}); ok {
				width = valueToInt(nested["width"])
				height = valueToInt(nested["height"])
				format = valueToString(nested["format"])
			}
		}
		if width > 0 && height > 0 {
			return &ImageProbe{Width: width, Height: height, Format: normalizeImageFormat(format)}, nil
		}
	}

	return nil, fmt.Errorf("无法解析 CI 图片信息: %s", truncateForLog(string(body), 300))
}

func valueToInt(v interface{}) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	case string:
		i, _ := strconv.Atoi(strings.TrimSpace(n))
		return i
	default:
		return 0
	}
}

func valueToString(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func normalizeImageFormat(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "jpg", "jpeg":
		return "jpeg"
	case "heic", "heix":
		return "heic"
	case "heif", "hevc", "hevx", "mif1", "msf1":
		return "heif"
	default:
		return strings.ToLower(strings.TrimSpace(raw))
	}
}

func truncateForLog(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
