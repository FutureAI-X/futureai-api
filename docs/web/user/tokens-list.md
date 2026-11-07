# API Key 列表

`GET /api/user/tokens`

返回当前用户的 API Key 列表。列表只包含 `key_prefix`、`key_suffix` 与 `key_length`，不返回完整 key。

完整 key 只在创建时出现一次，说明见 [API Key 的可见性](README.md#2-api-key-的可见性)。
