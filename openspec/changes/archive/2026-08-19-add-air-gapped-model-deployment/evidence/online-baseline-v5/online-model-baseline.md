# 线上模型工程基线

- 状态: engineering_online_model_baseline
- Gate: **passed**
- Endpoint: https://api.siliconflow.cn/v1
- Formal acceptance: **blocked** — 应用级知识库、50 个独立会话、正式专家标注和线上目标仍需专用 WeKnora 测试环境。
- Integrity SHA-256: dc6a624f86924a71f22d3e94935d68411ef0b4d9d8e19b5b178beeb8ce194e8b

| 检查项 | 状态 | 详情 |
| --- | --- | --- |
| model_catalog | passed | {"model_count":91,"selected":[{"role":"chat","model":"Qwen/Qwen3.6-27B","available":true},{"role":"verifier_1","model":"deepseek-ai/DeepSeek-V3.1-Terminus","available":true},{"role":"verifier_2","model":"Qwen/Qwen3.5-9B","available":true},{"role":"judge","model":"Qwen/Qwen3-32B","available":true},{"role":"embedding","model":"Qwen/Qwen3-Embedding-4B","available":true},{"role":"rerank","model":"BAAI/bge-reranker-v2-m3","available":true},{"role":"asr","model":"FunAudioLLM/SenseVoiceSmall","available":true},{"role":"tts","model":"FunAudioLLM/CosyVoice2-0.5B","available":true}]} |
| chat_chat | passed | {"first_visible_ms":251,"chunk_count":74,"answer_chars":128,"answer_preview":"离线环境缺乏网络连通性，无法实时获取或更新资源，因此必须通过镜像摘要和权重校验和确保部署内容的完整性与一致性，防止因数据损坏或篡改导致服务异常。同时，验证模型端点位置能确保服务路由正确指向已验证的本地资源，从而在隔离环境中实现安全、可靠且可追溯的模型运行。"} |
| chat_verifier_1 | passed | {"first_visible_ms":23121,"chunk_count":61,"answer_chars":106,"answer_preview":"离线部署必须验证模型端点位置、镜像摘要和模型权重校验和，以确保部署的模型与预期版本完全一致，避免因环境差异或文件损坏导致运行错误。这三项验证共同保障了模型的可复现性、安全性和稳定性，防止潜在的数据泄露或性能异常。"} |
| chat_verifier_2 | passed | {"first_visible_ms":489,"chunk_count":43,"answer_chars":79,"answer_preview":"验证模型端点位置确保服务在正确的网络环境中运行，防止连接错误。校验镜像摘要和模型权重则分别保障部署环境的完整性与模型文件未被篡改，共同构建可信的离线安全防线。"} |
| chat_judge | passed | {"first_visible_ms":15201,"chunk_count":64,"answer_chars":99,"answer_preview":"离线部署必须验证模型端点位置，以确保模型文件存储在正确且可访问的路径中。同时验证镜像摘要和模型权重校验和，是为了保证所使用的镜像和模型文件在传输和存储过程中未被篡改或损坏，确保部署的一致性和安全性。"} |
| embedding | passed | {"dimension":2560,"expected_dimension":2560} |
| rerank | passed | {"result_count":2,"top_index":0} |
| verification_verifier_1 | passed | {"first_visible_ms":12425,"chunk_count":31,"answer_chars":46,"answer_preview":"结论：不完整缺失项：未提及性能指标（如延迟、吞吐量）、量化精度损失、资源使用率（如显存占用）"} |
| verification_verifier_2 | passed | {"first_visible_ms":3477,"chunk_count":131,"answer_chars":238,"answer_preview":"结论：回答未覆盖问题要求，缺失关键指标。缺失项：1. **性能指标**：未提及延迟（Latency）、吞吐量（Throughput）或 QPS等核心性能验收口径。2. **资源指标**：未提及显存占用（VRAM Usage）或内存占用等量化部署特有的资源验收口径。3. **精度指标**：未提及量化后的模型精度（Accuracy/Top-1）与基线的一致性验收口径。4. **检查项缺失**：未给出具体的可操作检查项（如：对比基准测试脚本、显存监控命令、精度回归测试方法等）。"} |
| asr | passed | {"transcript_chars":22,"transcript":"Hello, this is a test.","audio_bytes":43088} |
| tts | passed | {"voice":"FunAudioLLM/CosyVoice2-0.5B:alex","format":"mp3","audio_bytes":47743,"output_file":"F:\\AI-Project\\WeKnora-development\\openspec\\changes\\add-air-gapped-model-deployment\\evidence\\online-baseline-v5\\online-tts-smoke.mp3"} |
| voice_answer | passed | {"first_visible_ms":974,"chunk_count":7,"answer_chars":22,"answer_preview":"Hello! This is a test."} |
| voice_followup | passed | {"first_visible_ms":216,"chunk_count":15,"answer_chars":26,"answer_preview":"上一轮对话仅为简单的问候与测试确认，无实质信息重点。"} |

## 说明

本报告只证明线上 OpenAI-compatible 模型端点和模型级语音链路可用；应用级知识库准确率、50 个独立会话、10 并发、专家标注和三种离线 profile 仍需专用验收环境。
