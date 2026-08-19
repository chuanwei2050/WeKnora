# 线上模型工程基线

- 状态: engineering_online_model_baseline
- Gate: **failed**
- Endpoint: https://api.siliconflow.cn/v1
- Formal acceptance: **blocked** — 应用级知识库、50 个独立会话、正式专家标注和线上目标仍需专用 WeKnora 测试环境。
- Integrity SHA-256: bf349bb8486468949cb510b6928a842c2e5103308488af82e19aa940c9a6e047

| 检查项 | 状态 | 详情 |
| --- | --- | --- |
| model_catalog | passed | {"model_count":91,"selected":[{"role":"chat","model":"Qwen/Qwen3.6-27B","available":true},{"role":"verifier_1","model":"deepseek-ai/DeepSeek-V3.1-Terminus","available":true},{"role":"verifier_2","model":"Qwen/Qwen3.5-9B","available":true},{"role":"judge","model":"Qwen/Qwen3-32B","available":true},{"role":"embedding","model":"Qwen/Qwen3-Embedding-4B","available":true},{"role":"rerank","model":"BAAI/bge-reranker-v2-m3","available":true},{"role":"asr","model":"FunAudioLLM/SenseVoiceSmall","available":true},{"role":"tts","model":"FunAudioLLM/CosyVoice2-0.5B","available":true}]} |
| chat_chat | passed | {"first_visible_ms":null,"chunk_count":0,"answer_chars":0,"answer_preview":""} |
| chat_verifier_1 | passed | {"first_visible_ms":679,"chunk_count":43,"answer_chars":74,"answer_preview":"离线部署时验证模型端点位置确保服务可访问，镜像摘要保证运行环境一致性，模型权重校验和则防止模型文件被篡改。这三者共同保障了离线部署的完整性和安全性。"} |
| chat_verifier_2 | passed | {"first_visible_ms":null,"chunk_count":0,"answer_chars":0,"answer_preview":""} |
| chat_judge | passed | {"first_visible_ms":1305,"chunk_count":75,"answer_chars":120,"answer_preview":"离线部署必须验证模型端点位置，以确保模型文件存储在正确且可访问的路径中；验证镜像摘要和模型权重校验和，则是为了保证所使用的镜像和模型文件在传输和存储过程中未被篡改，确保部署的一致性和安全性。这三者共同构成了模型部署的完整性与可靠性保障机制。"} |
| embedding | failed | {"dimension":2560,"expected_dimension":1024} error=Embedding 维度与冻结索引规格不一致。 |
| rerank | passed | {"result_count":2,"top_index":0} |
| verification_verifier_1 | passed | {"first_visible_ms":385,"chunk_count":18,"answer_chars":27,"answer_preview":"结论：回答不完整。缺失项：未列出任何具体指标或检查项。"} |
| verification_verifier_2 | passed | {"first_visible_ms":null,"chunk_count":0,"answer_chars":0,"answer_preview":""} |
| asr | passed | {"transcript_chars":22,"transcript":"Hello, this is a test.","audio_bytes":43088} |
| tts | passed | {"voice":"FunAudioLLM/CosyVoice2-0.5B:alex","format":"mp3","audio_bytes":47743,"output_file":"F:\\AI-Project\\WeKnora-development\\openspec\\changes\\add-air-gapped-model-deployment\\evidence\\online-baseline\\online-tts-smoke.mp3"} |
| voice_answer | passed | {"first_visible_ms":17674,"chunk_count":5,"answer_chars":8,"answer_preview":"你好，测试成功。"} |
| voice_followup | passed | {"first_visible_ms":null,"chunk_count":0,"answer_chars":0,"answer_preview":""} |

## 说明

本报告只证明线上 OpenAI-compatible 模型端点和模型级语音链路可用；应用级知识库准确率、50 个独立会话、10 并发、专家标注和三种离线 profile 仍需专用验收环境。
