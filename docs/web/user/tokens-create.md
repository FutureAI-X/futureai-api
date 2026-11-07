# 新建 API Key

`POST /api/user/tokens`

新建一个 API Key。响应中的完整 key 只出现一次，并用 `data_key` 加密。请当场保存。

`data_key` 的传递方式见 [登录与认证](../auth/README.md#2-data_key)。

列表接口只返回 key 的前后缀，因为数据库只保存哈希，无法还原完整 key。
