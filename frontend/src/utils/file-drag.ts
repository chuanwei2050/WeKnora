export const isFileDrag = (dataTransfer: DataTransfer | null): boolean =>
  Array.from(dataTransfer?.types ?? []).includes('Files')
