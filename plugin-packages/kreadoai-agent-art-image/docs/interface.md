# KreadoAI 文生图接口字段

设置里选择这个接口后，服务地址填写 MCP 端点，密钥栏填写用户自己的 apiToken。宿主把密钥放在请求头 `apiToken` 里发出，不写入地址。

## 协议身份

- 插件 ID：`kreadoai-agent-art-image`。
- Provider ID：`kreadoai-agent-art-image`。
- 能力：`image`。
- 默认 Base URL：`https://api.kreadoai.com/mcp/agentart/v1/mcp`。
- 鉴权驱动：`header`，请求头名 `apiToken`。
- 创建：MCP 工具 `submit_ai_image_task`。
- 查询：MCP 工具 `get_ai_image_task_detail`。

## 配置字段

| 字段 | 类型 | 必填 | 含义 |
| --- | --- | --- | --- |
| `apiKey` | secret | 是 | 作为请求头 `apiToken` 发送的密钥 |

## 统一字段映射

| 统一字段 | 类型 | 必填 | 上游映射 | 说明 |
| --- | --- | --- | --- | --- |
| `model` | string | 是 | `modelsSource` | 图片模型编号，例如 1022。 |
| `prompt` | string | 是 | `userPrompt` | 图片提示词。 |
| `imageCount` | integer | 否 | `quantity` | 输出数量，未填时为 1。 |

## 响应与错误

等待和执行中继续查询原任务。成功时取图片地址，由宿主下载后写入现有任务结果。失败保持失败，不包装成成功。

## 兼容边界

这一版只做文生图。带参考图的请求会在发出前提示去掉参考图。视频、音频和文本不走这个接口。

<!-- BEEFTV_PLUGIN_MANIFEST_START -->
## Manifest 完整接口定义

以下 JSON 与插件包内实际 `manifest.json` 逐字段一致，覆盖插件身份、权限、配置、鉴权、参数、校验、创建、Agent、查询、取消、结果下载、响应和 Agent 响应映射。`documentation` 字段的值就是当前完整文档；为避免文档在自身内部无限递归，JSON 中仅用等义占位文本表示正文。

```json
{
  "apiVersion": "beeftv.plugin/v2",
  "id": "kreadoai-agent-art-image",
  "name": "KreadoAI 文生图",
  "version": "1.0.0",
  "author": "BeefTV Contributors",
  "description": "通过 MCP 提交文生图，再查询原任务直到出图。",
  "documentation": "<当前插件的完整 documentation，由 README.md 与 docs/interface.md 拼接而成；为避免 JSON 递归，此处不重复展开正文。>",
  "permissions": [
    "generation.run",
    "media.read"
  ],
  "configuration": {
    "fields": [
      {
        "name": "apiKey",
        "type": "secret",
        "label": "apiToken",
        "required": true
      }
    ]
  },
  "contributes": {
    "providers": [
      {
        "id": "kreadoai-agent-art-image",
        "label": "KreadoAI 文生图",
        "capabilities": [
          "image"
        ],
        "scopes": [
          "admin.system-channel",
          "user.custom-channel",
          "canvas",
          "creation",
          "agent"
        ],
        "baseUrl": "https://api.kreadoai.com/mcp/agentart/v1/mcp",
        "presets": [
          {
            "id": "kreadoai-agent-art",
            "label": "KreadoAI 文生图",
            "baseUrl": "https://api.kreadoai.com/mcp/agentart/v1/mcp"
          }
        ],
        "auth": {
          "type": "header",
          "field": "apiKey",
          "header": "apiToken"
        },
        "parameters": [
          {
            "name": "model",
            "type": "string",
            "required": true,
            "mapping": "modelsSource",
            "description": "图片模型编号。"
          },
          {
            "name": "prompt",
            "type": "string",
            "required": true,
            "mapping": "userPrompt",
            "description": "图片提示词。"
          },
          {
            "name": "imageCount",
            "type": "integer",
            "required": false,
            "mapping": "quantity",
            "description": "输出数量。"
          }
        ],
        "models": [
          {
            "id": "1022",
            "label": "aihub_gpt-image-2.6-sunburst",
            "capability": "image"
          },
          {
            "id": "1023",
            "label": "aihub_gpt-image-2.6-flare",
            "capability": "image"
          },
          {
            "id": "1021",
            "label": "gpt-image-2.5-sunburst",
            "capability": "image"
          },
          {
            "id": "1020",
            "label": "gpt-image-2",
            "capability": "image"
          },
          {
            "id": "1013",
            "label": "doubao-seedream-5.5",
            "capability": "image"
          }
        ],
        "validations": [
          {
            "assert": {
              "$eq": [
                {
                  "$len": {
                    "$ref": "request.images"
                  }
                },
                0
              ]
            },
            "message": "这个接口目前只支持文生图，请去掉参考图后再试。"
          }
        ],
        "create": {
          "method": "POST",
          "path": "/",
          "transport": "mcp",
          "tool": "submit_ai_image_task",
          "contentType": "application/json",
          "body": {
            "userPrompt": {
              "$ref": "request.prompt"
            },
            "quantity": {
              "$if": {
                "condition": {
                  "$gt": [
                    {
                      "$ref": "request.imageCount"
                    },
                    0
                  ]
                },
                "then": {
                  "$ref": "request.imageCount"
                },
                "else": 1
              }
            },
            "modelsSource": {
              "$toInt": {
                "$ref": "request.model"
              }
            }
          }
        },
        "poll": {
          "method": "POST",
          "path": "/",
          "transport": "mcp",
          "tool": "get_ai_image_task_detail",
          "contentType": "application/json",
          "body": {
            "taskId": {
              "$ref": "taskId"
            }
          }
        },
        "response": {
          "taskId": {
            "$coalesce": [
              {
                "$ref": "response.taskId"
              },
              {
                "$ref": "response.task_id"
              },
              {
                "$ref": "response.id"
              },
              {
                "$ref": "response.data.taskId"
              },
              {
                "$ref": "response.data.id"
              }
            ]
          },
          "status": {
            "$switch": {
              "cases": [
                {
                  "when": {
                    "$eq": [
                      {
                        "$ref": "response.status"
                      },
                      1
                    ]
                  },
                  "then": "pending"
                },
                {
                  "when": {
                    "$eq": [
                      {
                        "$ref": "response.status"
                      },
                      2
                    ]
                  },
                  "then": "processing"
                },
                {
                  "when": {
                    "$eq": [
                      {
                        "$ref": "response.status"
                      },
                      3
                    ]
                  },
                  "then": "succeeded"
                },
                {
                  "when": {
                    "$eq": [
                      {
                        "$ref": "response.status"
                      },
                      4
                    ]
                  },
                  "then": "failed"
                },
                {
                  "when": {
                    "$eq": [
                      {
                        "$toString": {
                          "$ref": "response.status"
                        }
                      },
                      "1"
                    ]
                  },
                  "then": "pending"
                },
                {
                  "when": {
                    "$eq": [
                      {
                        "$toString": {
                          "$ref": "response.status"
                        }
                      },
                      "2"
                    ]
                  },
                  "then": "processing"
                },
                {
                  "when": {
                    "$eq": [
                      {
                        "$toString": {
                          "$ref": "response.status"
                        }
                      },
                      "3"
                    ]
                  },
                  "then": "succeeded"
                },
                {
                  "when": {
                    "$eq": [
                      {
                        "$toString": {
                          "$ref": "response.status"
                        }
                      },
                      "4"
                    ]
                  },
                  "then": "failed"
                }
              ],
              "default": {
                "$ref": "response.status"
              }
            }
          },
          "message": {
            "$coalesce": [
              {
                "$ref": "response.message"
              },
              {
                "$ref": "response.error.message"
              },
              {
                "$ref": "response.data.message"
              }
            ]
          },
          "images": {
            "$coalesce": [
              {
                "$ref": "response.images"
              },
              {
                "$ref": "response.data.images"
              },
              {
                "$ref": "response.imageUrl"
              },
              {
                "$ref": "response.data.imageUrl"
              }
            ]
          },
          "resultEphemeral": true,
          "errorPaths": [
            "error.code"
          ],
          "messagePaths": [
            "error.message",
            "message"
          ]
        }
      }
    ]
  }
}
```
<!-- BEEFTV_PLUGIN_MANIFEST_END -->
