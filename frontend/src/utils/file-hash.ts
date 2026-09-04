export async function calculateFileMD5(file: Blob): Promise<string> {
  const { createMD5 } = await import('hash-wasm')
  const hasher = await createMD5()
  const chunkSize = 2 * 1024 * 1024

  for (let offset = 0; offset < file.size; offset += chunkSize) {
    const chunk = await readBlob(file.slice(offset, offset + chunkSize))
    hasher.update(new Uint8Array(chunk))
  }

  return hasher.digest()
}

function readBlob(blob: Blob): Promise<ArrayBuffer> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onerror = () => reject(reader.error || new Error('Failed to read file'))
    reader.onload = () => {
      if (reader.result instanceof ArrayBuffer) {
        resolve(reader.result)
      } else {
        reject(new Error('Unexpected file reader result'))
      }
    }
    reader.readAsArrayBuffer(blob)
  })
}
