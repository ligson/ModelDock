export function sourceURL(source: string, id: string): string {
  const clean = id.replace(/^hf:/i, '')
  if (/^https?:\/\//i.test(clean)) return clean
  if (source === 'huggingface' || source === 'hf') return `https://huggingface.co/${clean}`
  if (source === 'modelscope') return `https://modelscope.cn/models/${clean}`
  if (source === 'ollama') return `https://ollama.com/library/${clean.split(':')[0]}`
  return ''
}
