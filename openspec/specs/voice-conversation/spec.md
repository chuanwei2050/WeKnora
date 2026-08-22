# voice-conversation Specification

## Purpose
TBD - created by archiving change add-voice-conversation-loop. Update Purpose after archive.
## Requirements
### Requirement: 语音连续交互基线
正式语音验收 MUST 验证同一会话内的“语音提问—最终转写—文本回答—TTS 播放—主动中断—继续追问”闭环，并记录 ASR/TTS 模型身份、文本首字时间、音频首块时间、播放中断和临时资源清理结果。

#### Scenario: 语音问答后继续追问
- **WHEN** 用户完成一轮语音提问并收到文本和语音回答后再次显式开始录音
- **THEN** 第二轮请求复用同一会话上下文并能引用第一轮最终转写和回答
- **AND** 验收结果包含两轮的模型、时序和资源清理记录

#### Scenario: 播放中主动中断
- **WHEN** 用户在 TTS 播放期间停止播放并开始新的语音提问
- **THEN** 当前音频请求和播放被取消
- **AND** 已持久化文本回答不被删除或改写，临时音频资源被释放

