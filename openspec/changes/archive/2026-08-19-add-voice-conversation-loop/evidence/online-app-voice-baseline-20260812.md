# 线上应用级语音基线

- 状态：**passed**
- profile：`online-app`
- 链路：ASR → 文本回答 → TTS → 同一会话继续追问 → 再次 TTS
- ASR：`FunAudioLLM/SenseVoiceSmall`
- Chat：`Qwen/Qwen3.6-27B`
- TTS：`FunAudioLLM/CosyVoice2-0.5B`，音色 `FunAudioLLM/CosyVoice2-0.5B:alex`

首轮和继续追问均在同一会话完成。两轮 ASR、文本回答和 TTS 都返回成功；TTS 音频分别为 113407 和 125695 字节。会话消息中保留文本回答，未创建音频附件，TTS 音频未写入持久化对象。

本报告是专用测试租户上的应用级基线，不代表内网部署验收；内网 profile 仍需使用同一冻结套件在目标服务器重跑。
