import { useState, useRef, useEffect, useCallback } from 'react'
import styles from './Chat.module.css'

interface Message {
  role: 'user' | 'assistant'
  content: string
}

export default function Chat() {
  const [messages, setMessages] = useState<Message[]>([])
  const [input, setInput] = useState('')
  const [streaming, setStreaming] = useState(false)
  const bottomRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLTextAreaElement>(null)

  const scrollToBottom = useCallback(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [])

  useEffect(() => { scrollToBottom() }, [messages, scrollToBottom])

  const sendMessage = async () => {
    const text = input.trim()
    if (!text || streaming) return

    const userMsg: Message = { role: 'user', content: text }
    const history = messages.map(m => ({ role: m.role, content: m.content }))
    setMessages(prev => [...prev, userMsg])
    setInput('')
    setStreaming(true)

    // Add empty assistant message to fill incrementally
    setMessages(prev => [...prev, { role: 'assistant', content: '' }])

    try {
      const res = await fetch('/agent/chat/stream', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ message: text, history }),
      })

      if (!res.ok) {
        const err = await res.json().catch(() => ({ error: '请求失败' }))
        setMessages(prev => {
          const copy = [...prev]
          copy[copy.length - 1] = { role: 'assistant', content: `错误: ${err.error || res.statusText}` }
          return copy
        })
        setStreaming(false)
        return
      }

      const reader = res.body?.getReader()
      const decoder = new TextDecoder()
      let buffer = ''
      let fullContent = ''

      if (reader) {
        while (true) {
          const { done, value } = await reader.read()
          if (done) break

          buffer += decoder.decode(value, { stream: true })
          const lines = buffer.split('\n')
          buffer = lines.pop() || ''

          for (const line of lines) {
            if (line.startsWith('data: ')) {
              try {
                const data = JSON.parse(line.slice(6))
                if (data.type === 'content' && data.content) {
                  fullContent += data.content
                  setMessages(prev => {
                    const copy = [...prev]
                    copy[copy.length - 1] = { role: 'assistant', content: fullContent }
                    return copy
                  })
                } else if (data.type === 'error') {
                  fullContent += `\n\n错误: ${data.error}`
                  setMessages(prev => {
                    const copy = [...prev]
                    copy[copy.length - 1] = { role: 'assistant', content: fullContent }
                    return copy
                  })
                }
              } catch { /* ignore parse errors */ }
            }
          }
        }
      }

      // If no streaming content, try sync fallback
      if (!fullContent) {
        const syncRes = await fetch('/agent/chat', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ message: text, history }),
        })
        const syncData = await syncRes.json()
        setMessages(prev => {
          const copy = [...prev]
          copy[copy.length - 1] = { role: 'assistant', content: syncData.answer || syncData.error || '无回复' }
          return copy
        })
      }
    } catch (err) {
      setMessages(prev => {
        const copy = [...prev]
        copy[copy.length - 1] = { role: 'assistant', content: `网络错误: ${err}` }
        return copy
      })
    } finally {
      setStreaming(false)
      inputRef.current?.focus()
    }
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      sendMessage()
    }
  }

  return (
    <div className={styles.root}>
      <h1 className="wiki-h1">AI 代码助手</h1>
      <p className="muted text-sm mb-2">
        基于知识图谱的代码分析对话。AI 可以搜索代码、追踪调用链、解释实现逻辑。
      </p>

      <div className={styles.chatArea}>
        {messages.length === 0 && (
          <div className={styles.empty}>
            <p>👋 你好！我是 Sourcelex 代码助手。</p>
            <p className="muted text-sm">试试问我：</p>
            <ul className={styles.suggestions}>
              <li onClick={() => { setInput('StoreEntities 函数的作用是什么？'); }}>StoreEntities 函数的作用是什么？</li>
              <li onClick={() => { setInput('哪些函数调用了 SemanticSearch？'); }}>哪些函数调用了 SemanticSearch？</li>
              <li onClick={() => { setInput('解释一下 analyzer 的工作流程'); }}>解释一下 analyzer 的工作流程</li>
            </ul>
          </div>
        )}

        {messages.map((msg, i) => (
          <div key={i} className={`${styles.message} ${styles[msg.role]}`}>
            <div className={styles.avatar}>
              {msg.role === 'user' ? '👤' : '🤖'}
            </div>
            <div className={styles.bubble}>
              <div className={styles.sender}>
                {msg.role === 'user' ? '你' : 'Sourcelex AI'}
              </div>
              <div className={styles.content}>
                {renderMarkdown(msg.content)}
              </div>
            </div>
          </div>
        ))}

        {streaming && (
          <div className={styles.typing}>
            <span />
            <span />
            <span />
          </div>
        )}

        <div ref={bottomRef} />
      </div>

      <div className={styles.inputArea}>
        <textarea
          ref={inputRef}
          className={styles.input}
          value={input}
          onChange={e => setInput(e.target.value)}
          onKeyDown={handleKeyDown}
          placeholder="输入问题... (Enter 发送, Shift+Enter 换行)"
          rows={2}
          disabled={streaming}
        />
        <button
          className={styles.sendBtn}
          onClick={sendMessage}
          disabled={streaming || !input.trim()}
        >
          {streaming ? '思考中...' : '发送'}
        </button>
      </div>
    </div>
  )
}

// Simple markdown-like rendering
function renderMarkdown(text: string) {
  if (!text) return null

  const parts = text.split(/(```[\s\S]*?```|`[^`]+`)/g)

  return parts.map((part, i) => {
    if (part.startsWith('```') && part.endsWith('```')) {
      const code = part.slice(3, -3).replace(/^\w+\n/, '')
      return <pre key={i} className={styles.codeBlock}><code>{code}</code></pre>
    }
    if (part.startsWith('`') && part.endsWith('`')) {
      return <code key={i}>{part.slice(1, -1)}</code>
    }
    // Convert **bold** and line breaks
    const html = part
      .replace(/\*\*(.*?)\*\*/g, '<strong>$1</strong>')
      .replace(/\n/g, '<br/>')
    return <span key={i} dangerouslySetInnerHTML={{ __html: html }} />
  })
}
