import { afterEach, describe, expect, it, vi } from 'vitest'
import { streamChat } from './api'
import { subscribeEvents } from './events'

// In the desktop app the shell relays the daemon's events as Wails events,
// because on Windows a stream read through the app arrives only when it ends.

type Handler = (...data: unknown[]) => void
const w = window as unknown as { go?: unknown; runtime?: unknown }

/** Pretends to be the desktop shell; send() relays one event's JSON. */
function shell(eventName = 'toskar:event') {
  // The page listens to the event's name and the one from before the rename
  // (#237); the shell sends on one of them.
  const handlers = new Map<string, Set<Handler>>()
  w.go = { main: { App: { HasEventRelay: vi.fn().mockResolvedValue(true) } } }
  w.runtime = {
    EventsOn: (name: string, handler: Handler) => {
      expect(['toskar:event', 'ygg:event']).toContain(name)
      if (!handlers.has(name)) handlers.set(name, new Set())
      handlers.get(name)!.add(handler)
      return () => handlers.get(name)!.delete(handler)
    },
  }
  return {
    send: (event: unknown) => handlers.get(eventName)?.forEach((h) => h(JSON.stringify(event))),
    listening: () => handlers.get(eventName)?.size ?? 0,
  }
}

/** A chat response that streams the given SSE text once fetch is called. */
function chatResponse(sse: string) {
  return vi.fn().mockResolvedValue(
    new Response(sse, { status: 200, headers: { 'Content-Type': 'text/event-stream' } }),
  )
}

const token = (conversation: string, content: string) => ({ type: 'chat.token', payload: { conversation_id: conversation, content } })

afterEach(() => {
  delete w.go
  delete w.runtime
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

describe('subscribeEvents', () => {
  it('listens to the shell’s relay in the desktop app', () => {
    const relay = shell()
    const onEvent = vi.fn()
    const onOpen = vi.fn()
    const stop = subscribeEvents({ onEvent, onOpen })
    relay.send({ type: 'tool.started', payload: { tool_id: 'internet.search' } })
    expect(onOpen).toHaveBeenCalledTimes(1)
    expect(onEvent).toHaveBeenCalledWith({ type: 'tool.started', payload: { tool_id: 'internet.search' } })
    stop()
    expect(relay.listening()).toBe(0)
  })

  it('hears a shell from before the rename, which sends ygg:event', () => {
    const relay = shell('ygg:event')
    const onEvent = vi.fn()
    const stop = subscribeEvents({ onEvent })
    relay.send({ type: 'tool.started', payload: { tool_id: 'internet.search' } })
    expect(onEvent).toHaveBeenCalledTimes(1)
    stop()
    expect(relay.listening()).toBe(0)
  })
})

describe('streamChat with the relay', () => {
  it('takes the reply’s text from the relay and finishes after chat.complete', async () => {
    const relay = shell()
    // The response repeats the text; with the relay it only says when the reply is done.
    vi.stubGlobal('fetch', chatResponse('event: token\ndata: {"content":"ignored"}\n\nevent: done\ndata: {}\n\n'))
    const tokens: string[] = []
    const onDone = vi.fn()
    const pending = streamChat({ body: { conversation_id: 'c1', message: 'hi' }, onToken: (t) => tokens.push(t), onDone })

    relay.send(token('c1', 'Hel'))
    relay.send(token('other', 'not this chat'))
    relay.send(token('c1', 'lo'))
    await vi.waitFor(() => expect(tokens).toEqual(['Hel', 'lo']))
    // Let the response finish reading; the relay still hasn't said the reply is complete.
    await new Promise((resolve) => setTimeout(resolve, 50))
    expect(onDone).not.toHaveBeenCalled()

    relay.send(token('c1', '!')) // a last token that arrives after the response ended
    relay.send({ type: 'chat.complete', payload: { conversation_id: 'c1' } })
    await pending
    expect(tokens).toEqual(['Hel', 'lo', '!'])
    expect(onDone).toHaveBeenCalledTimes(1)
    expect(relay.listening()).toBe(0)
  })

  it('finishes after a short wait if chat.complete never comes', async () => {
    shell()
    vi.useFakeTimers({ shouldAdvanceTime: true })
    vi.stubGlobal('fetch', chatResponse('event: done\ndata: {}\n\n'))
    const onDone = vi.fn()
    const pending = streamChat({ body: { conversation_id: 'c1', message: 'hi' }, onToken: () => {}, onDone })
    await vi.advanceTimersByTimeAsync(3100)
    await pending
    expect(onDone).toHaveBeenCalledTimes(1)
  })

  it('reports an error at once', async () => {
    const relay = shell()
    vi.stubGlobal('fetch', chatResponse('event: error\ndata: The model failed.\n\n'))
    const onError = vi.fn()
    const onDone = vi.fn()
    await streamChat({ body: { conversation_id: 'c1', message: 'hi' }, onToken: () => {}, onDone, onError })
    expect(onError).toHaveBeenCalledWith('The model failed.', undefined)
    expect(onDone).not.toHaveBeenCalled()
    expect(relay.listening()).toBe(0)
  })

  it('reads the response’s tokens without the relay', async () => {
    vi.stubGlobal('fetch', chatResponse('event: token\ndata: {"content":"Hi"}\n\nevent: done\ndata: {}\n\n'))
    const tokens: string[] = []
    const onDone = vi.fn()
    await streamChat({ body: { conversation_id: 'c1', message: 'hi' }, onToken: (t) => tokens.push(t), onDone })
    expect(tokens).toEqual(['Hi'])
    expect(onDone).toHaveBeenCalledTimes(1)
  })
})
