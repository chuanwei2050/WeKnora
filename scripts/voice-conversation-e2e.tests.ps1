$ErrorActionPreference = 'Stop'
$script = Join-Path $PSScriptRoot 'voice-conversation-e2e.ps1'
$token = Join-Path $PSScriptRoot 'testdata/voice-e2e-token.test'
$audio = Join-Path $PSScriptRoot 'testdata/voice-e2e-audio.test'
Set-Content -LiteralPath $token -Value 'test-token' -Encoding ascii
Set-Content -LiteralPath $audio -Value 'audio' -Encoding ascii
try {
  foreach ($arguments in @(
    @('-Target', 'http://127.0.0.1:8081'),
    @('-Target', 'http://production.example.invalid', '-ConfirmTestEnvironment'),
    @('-Target', 'not-a-url', '-ConfirmTestEnvironment')
  )) {
    try {
      & $script -TokenFile $token -AudioFile $audio -ASRModel 'asr' -TTSModel 'tts' -Voice 'voice' -TimeoutSeconds 1 @arguments 2>&1 | Out-Null
      throw "语音 E2E 安全边界测试应失败: $($arguments -join ' ')"
    } catch {
      if ($_.Exception.Message -like '语音 E2E 安全边界测试应失败*') { throw }
    }
  }
} finally {
  Remove-Item -LiteralPath $token, $audio -Force -ErrorAction SilentlyContinue
}
Write-Output 'voice-conversation-e2e safety tests passed'
exit 0
