import { describe, expect, it } from 'vitest'

import { readDrop } from './dropped-files'

function fileEntry(name: string, contents = 'x'): FileSystemEntry {
  return {
    name,
    isFile: true,
    isDirectory: false,
    file: (resolve: (file: File) => void) => resolve(new File([contents], name)),
  } as unknown as FileSystemEntry
}

function directoryEntry(name: string, children: FileSystemEntry[]): FileSystemEntry {
  let served = false
  return {
    name,
    isFile: false,
    isDirectory: true,
    createReader: () => ({
      // The real API hands back batches and signals the end with an empty one.
      readEntries: (resolve: (entries: FileSystemEntry[]) => void) => {
        resolve(served ? [] : children)
        served = true
      },
    }),
  } as unknown as FileSystemEntry
}

function drop(entries: FileSystemEntry[]): DataTransferItemList {
  return entries.map((entry) => ({
    webkitGetAsEntry: () => entry,
  })) as unknown as DataTransferItemList
}

describe('readDrop', () => {
  it('keeps a dropped file under its own name', async () => {
    const files = await readDrop(drop([fileEntry('ingress.yaml')]), null)

    expect(files.map((f) => f.path)).toEqual(['ingress.yaml'])
  })

  // A chart is a directory, and flattening it breaks every relative path inside
  // Chart.yaml and templates/.
  it('keeps the structure of a dropped folder', async () => {
    const chart = directoryEntry('mychart', [
      fileEntry('Chart.yaml'),
      fileEntry('values.yaml'),
      directoryEntry('templates', [fileEntry('deployment.yaml'), fileEntry('service.yaml')]),
    ])

    const files = await readDrop(drop([chart]), null)

    expect(files.map((f) => f.path).sort()).toEqual([
      'mychart/Chart.yaml',
      'mychart/templates/deployment.yaml',
      'mychart/templates/service.yaml',
      'mychart/values.yaml',
    ])
  })

  it('falls back to plain names where the entry API is missing', async () => {
    const picked = [new File(['x'], 'values.yaml')] as unknown as FileList

    const files = await readDrop(null, picked)

    expect(files.map((f) => f.path)).toEqual(['values.yaml'])
  })
})
