# 公开图片上传

`POST /v1/uploads/images`

旧版图片上传接口。使用 `multipart/form-data`，字段名 `file`。请求需要 `Authorization: Bearer sk-...`。

## 1 限制

单文件上限 10MB，超过返回 `413`。网关侧的 `client_max_body_size` 为 12m。若请求体超过该值，请求会在到达应用之前被拒绝。

## 2 响应

默认把图片转存到平台自己的对象存储，成功响应如下。

```json
{
  "url": "https://futureaiapi.com/v1/images/get/file_6d41e4bf9a4bfac7b1ff97d7.png",
  "filename": "photo.png",
  "content_type": "image/png",
  "bytes": 123456
}
```

`url` 是公开读取地址，无需认证，有效期与成图一致。`filename` 是调用方上传时的文件名，下载时实际文件名取自 URL 末段。

服务端可通过 `IMAGE_UPLOAD_BACKEND=apimart` 切回第三方上传。此时 `url` 是第三方的托管地址，其余字段不变。

## 3 与参考媒体上传的区别

该接口返回公开 URL，适合不需要私有文件的场景。需要在生成请求中使用私有参考文件时，应使用 [申请上传凭证](reference-media-prepare.md) 与 [完成上传](reference-media-complete.md) 流程。
