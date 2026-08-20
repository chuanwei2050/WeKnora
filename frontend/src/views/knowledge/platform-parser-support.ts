export interface PlatformParserEngine {
  FileTypes?: string[]
  Available?: boolean
}

export function collectPlatformSupportedFileTypes(engines: PlatformParserEngine[]): Set<string> {
  const supported = new Set<string>()
  for (const engine of engines) {
    if (engine.Available === false) continue
    for (const fileType of engine.FileTypes || []) {
      supported.add(fileType)
    }
  }
  return supported
}
