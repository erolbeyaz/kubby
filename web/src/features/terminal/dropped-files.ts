/** One file on its way into a session, with the path it should keep. */
export interface DroppedFile {
  path: string
  file: File
}

/**
 * Reads what was dropped, following folders down.
 *
 * A Helm chart is a directory, and flattening it would break every relative path inside
 * `Chart.yaml` and `templates/`. The relative structure is kept so the chart arrives as
 * the chart it is.
 */
export async function readDrop(items: DataTransferItemList | null, fallback: FileList | null): Promise<DroppedFile[]> {
  const entries: FileSystemEntry[] = []

  if (items) {
    for (const item of Array.from(items)) {
      // Must be read synchronously: the DataTransfer is emptied once the handler yields.
      const entry = item.webkitGetAsEntry?.()
      if (entry) entries.push(entry)
    }
  }

  if (entries.length > 0) {
    const collected: DroppedFile[] = []
    for (const entry of entries) {
      await walk(entry, '', collected)
    }
    return collected
  }

  // A browser without the entry API, or a file chosen through a picker rather than
  // dropped: names only, no folders.
  return Array.from(fallback ?? []).map((file) => ({ path: file.name, file }))
}

async function walk(entry: FileSystemEntry, prefix: string, into: DroppedFile[]): Promise<void> {
  const path = prefix ? `${prefix}/${entry.name}` : entry.name

  if (entry.isFile) {
    const file = await new Promise<File | null>((resolve) => {
      ;(entry as FileSystemFileEntry).file(resolve, () => resolve(null))
    })
    if (file) into.push({ path, file })
    return
  }

  if (!entry.isDirectory) return

  if (into.length >= MAX_FILES) return

  const reader = (entry as FileSystemDirectoryEntry).createReader()
  // readEntries returns at most a hundred at a time and signals the end with an empty
  // batch, so one call would silently truncate a large chart.
  for (;;) {
    const batch = await new Promise<FileSystemEntry[]>((resolve) => {
      reader.readEntries(resolve, () => resolve([]))
    })
    if (batch.length === 0) return

    for (const child of batch) {
      if (into.length >= MAX_FILES) return
      await walk(child, path, into)
    }
  }
}

/** Enough for a chart, far short of someone dropping their home directory in. */
export const MAX_FILES = 400
