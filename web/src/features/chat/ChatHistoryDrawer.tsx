import { useEffect, useMemo, useState, type MouseEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { useDialog } from '@/lib/useDialog'
import { LoadingSpinner } from '@/components/ui/LoadingSpinner'
import type { Conversation } from '@/types/api'
import { rovingKeyDown, useMenu } from '@/lib/roving'

const PIN_MIN_WIDTH = 1100

export function useCanPinChatHistory(): boolean {
  const [canPin, setCanPin] = useState(() =>
    typeof window !== 'undefined' ? window.innerWidth >= PIN_MIN_WIDTH : true,
  )

  useEffect(() => {
    const mq = window.matchMedia(`(min-width: ${PIN_MIN_WIDTH}px)`)
    const sync = () => setCanPin(mq.matches)
    sync()
    mq.addEventListener('change', sync)
    return () => mq.removeEventListener('change', sync)
  }, [])

  return canPin
}

// Groups, in order; their names are chat:history.groups.<group> in the catalog.
type Group = 'pinned' | 'today' | 'yesterday' | 'week' | 'month' | 'older'

function recencyGroup(iso: string): Group {
  const d = new Date(iso)
  const now = new Date()
  const startToday = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  const startYesterday = new Date(startToday)
  startYesterday.setDate(startYesterday.getDate() - 1)
  const start7 = new Date(startToday)
  start7.setDate(start7.getDate() - 7)
  const start30 = new Date(startToday)
  start30.setDate(start30.getDate() - 30)

  if (d >= startToday) return 'today'
  if (d >= startYesterday) return 'yesterday'
  if (d >= start7) return 'week'
  if (d >= start30) return 'month'
  return 'older'
}

const GROUP_ORDER: Group[] = ['pinned', 'today', 'yesterday', 'week', 'month', 'older']

function groupConversations(
  items: Conversation[],
  pinnedIds: string[],
): { label: Group; items: Conversation[] }[] {
  const pinnedSet = new Set(pinnedIds)
  const pinned: Conversation[] = []
  const rest: Conversation[] = []
  for (const c of items) {
    if (pinnedSet.has(c.id)) pinned.push(c)
    else rest.push(c)
  }

  const map = new Map<Group, Conversation[]>()
  if (pinned.length > 0) {
    map.set('pinned', pinned)
  }
  for (const c of rest) {
    const label = recencyGroup(c.updated_at || c.created_at)
    if (!map.has(label)) map.set(label, [])
    map.get(label)!.push(c)
  }

  return GROUP_ORDER.filter((label) => map.has(label)).map((label) => ({
    label,
    items: map.get(label)!,
  }))
}

type ChatHistoryDrawerProps = {
  open: boolean
  mode: 'overlay' | 'pinned'
  conversations: Conversation[]
  pinnedIds: string[]
  selectedId: string | null
  loading?: boolean
  error?: string | null
  onClose: () => void
  onSelect: (id: string) => void
  onNewChat: () => void
  onRename: (conversation: Conversation) => void
  onTogglePin: (id: string) => void
  onDelete: (conversation: Conversation, event: MouseEvent) => void
  onToggleDrawerPinned: () => void
  drawerPinned: boolean
  canPinDrawer: boolean
  renamingId: string | null
  renameValue: string
  onRenameValueChange: (value: string) => void
  onCommitRename: () => void
  onCancelRename: () => void
}

export function ChatHistoryDrawer({
  open,
  mode,
  conversations,
  pinnedIds,
  selectedId,
  loading,
  error,
  onClose,
  onSelect,
  onNewChat,
  onRename,
  onTogglePin,
  onDelete,
  onToggleDrawerPinned,
  drawerPinned,
  canPinDrawer,
  renamingId,
  renameValue,
  onRenameValueChange,
  onCommitRename,
  onCancelRename,
}: ChatHistoryDrawerProps) {
  const { t } = useTranslation('chat')
  const [search, setSearch] = useState('')
  const [menuOpenId, setMenuOpenId] = useState<string | null>(null)
  const menuRef = useMenu(menuOpenId, () => setMenuOpenId(null))
  // As an overlay it is a modal dialog: focus moves in and stays, and Escape
  // closes it. Pinned, it is a side panel the page works alongside.
  const overlay = mode === 'overlay'
  const panelRef = useDialog<HTMLElement>(open && overlay, onClose)

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase()
    if (!q) return conversations
    return conversations.filter((c) => c.title.toLowerCase().includes(q))
  }, [conversations, search])

  const grouped = useMemo(
    () => groupConversations(filtered, pinnedIds),
    [filtered, pinnedIds],
  )

  useEffect(() => {
    if (!menuOpenId) return
    const close = () => setMenuOpenId(null)
    window.addEventListener('click', close)
    return () => window.removeEventListener('click', close)
  }, [menuOpenId])

  if (!open) return null

  const panel = (
    <aside
      ref={panelRef}
      id="chat-history-drawer"
      className={[
        'chat-history-drawer flex min-h-0 w-[min(20rem,100%)] flex-col bg-surface',
        mode === 'overlay'
          ? 'absolute inset-y-0 start-0 z-30 border-e border-line/40 shadow-panel animate-chat-drawer'
          : 'relative z-10 h-full shrink-0 border-e border-line/50',
      ].join(' ')}
      role={overlay ? 'dialog' : undefined}
      aria-modal={overlay ? true : undefined}
      aria-label={t('history.label')}
    >
      <div className="flex shrink-0 items-center gap-2 border-b border-line/40 px-3 py-3">
        <h2 className="flex-1 font-display text-base font-semibold text-ink">{t('history.title')}</h2>
        {canPinDrawer && (
          <button
            type="button"
            className={[
              'rounded-md px-2 py-1 text-xs transition',
              drawerPinned
                ? 'bg-primary-soft text-primary-active'
                : 'text-ink-faint hover:bg-raised hover:text-ink',
            ].join(' ')}
            title={drawerPinned ? t('history.unpinSidebar') : t('history.pinSidebar')}
            aria-pressed={drawerPinned}
            onClick={onToggleDrawerPinned}
          >
            {drawerPinned ? t('history.pinned') : t('history.pin')}
          </button>
        )}
        <button
          type="button"
          className="rounded-md px-2 py-1 text-sm text-ink-faint hover:bg-raised hover:text-ink"
          title={t('history.close')}
          aria-label={t('history.close')}
          onClick={onClose}
        >
          ×
        </button>
      </div>

      <div className="shrink-0 px-3 pt-3">
        <label className="block">
          <span className="sr-only">{t('history.search')}</span>
          <input
            type="search"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder={t('history.searchPlaceholder')}
            className="field w-full py-2 text-sm"
            data-autofocus={overlay ? true : undefined}
          />
        </label>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto overflow-x-hidden px-2 py-3">
        {loading && <LoadingSpinner label={t('history.loading')} />}
        {error && (
          <p className="px-2 text-xs text-danger" role="alert">
            {error}
          </p>
        )}

        {!loading && conversations.length === 0 && (
          <div className="px-2 py-6 text-center">
            <p className="text-sm text-ink-muted">{t('history.empty')}</p>
            <button type="button" className="btn-primary mt-4 text-xs" onClick={onNewChat}>
              {t('history.startNew')}
            </button>
          </div>
        )}

        {!loading && conversations.length > 0 && grouped.length === 0 && (
          <p className="px-2 text-xs text-ink-faint">{t('history.noMatches')}</p>
        )}

        {grouped.map((group) => (
          <div key={group.label} className="mb-4">
            <p className="mb-1 px-2 text-[11px] font-medium uppercase tracking-wide text-ink-faint">
              {t(`history.groups.${group.label}`)}
            </p>
            <ul className="space-y-0.5">
              {group.items.map((conversation) => {
                const isSelected = selectedId === conversation.id
                const isPinned = pinnedIds.includes(conversation.id)
                return (
                  <li key={conversation.id} className="group relative">
                    {renamingId === conversation.id ? (
                      <input
                        className="field w-full py-1.5 text-sm"
                        value={renameValue}
                        autoFocus
                        onChange={(e) => onRenameValueChange(e.target.value)}
                        onBlur={onCommitRename}
                        onKeyDown={(e) => {
                          if (e.key === 'Enter') onCommitRename()
                          if (e.key === 'Escape') onCancelRename()
                        }}
                      />
                    ) : (
                      <>
                        <button
                          type="button"
                          title={conversation.title}
                          onClick={() => onSelect(conversation.id)}
                          className={[
                            'w-full rounded-lg py-2 ps-2.5 pe-9 text-start text-sm transition',
                            isSelected
                              ? 'bg-primary-soft font-medium text-primary-active'
                              : 'text-ink-muted hover:bg-raised/80 hover:text-ink',
                            isPinned && !isSelected ? 'text-ink' : '',
                          ].join(' ')}
                        >
                          <span className="flex min-w-0 items-start gap-1.5">
                            {isPinned ? (
                              <span
                                className="mt-1.5 h-1.5 w-1.5 shrink-0 rounded-full bg-accent"
                                aria-hidden
                                title={t('history.pinned')}
                              />
                            ) : null}
                            <span className="line-clamp-2 min-w-0">
                              {conversation.title || t('history.untitled')}
                            </span>
                          </span>
                        </button>
                        <div className="absolute end-1 top-1/2 -translate-y-1/2">
                          <button
                            type="button"
                            className={[
                              'rounded px-1.5 py-0.5 text-xs text-ink-faint hover:bg-raised hover:text-ink',
                              isSelected || menuOpenId === conversation.id
                                ? 'opacity-100'
                                : 'opacity-0 group-hover:opacity-100 focus:opacity-100',
                            ].join(' ')}
                            aria-label={t('history.actionsFor', { title: conversation.title || t('history.chat') })}
                            aria-expanded={menuOpenId === conversation.id}
                            onClick={(e) => {
                              e.stopPropagation()
                              setMenuOpenId((id) =>
                                id === conversation.id ? null : conversation.id,
                              )
                            }}
                          >
                            ···
                          </button>
                          {menuOpenId === conversation.id && (
                            <div
                              className="absolute end-0 top-full z-40 mt-1 min-w-[8.5rem] rounded-lg border border-line bg-surface py-1 shadow-panel"
                              ref={menuRef}
                              role="menu"
                              onKeyDown={rovingKeyDown}
                              onClick={(e) => e.stopPropagation()}
                            >
                              <button
                                type="button"
                                role="menuitem"
                                className="block w-full px-3 py-1.5 text-start text-sm text-ink hover:bg-raised"
                                onClick={(e) => {
                                  setMenuOpenId(null)
                                  onRename(conversation)
                                  e.stopPropagation()
                                }}
                              >
                                {t('history.rename')}
                              </button>
                              <button
                                type="button"
                                role="menuitem"
                                className="block w-full px-3 py-1.5 text-start text-sm text-ink hover:bg-raised"
                                onClick={(e) => {
                                  setMenuOpenId(null)
                                  onTogglePin(conversation.id)
                                  e.stopPropagation()
                                }}
                              >
                                {isPinned ? t('history.unpin') : t('history.pin')}
                              </button>
                              <button
                                type="button"
                                role="menuitem"
                                className="block w-full px-3 py-1.5 text-start text-sm text-danger hover:bg-danger/10"
                                onClick={(e) => {
                                  setMenuOpenId(null)
                                  onDelete(conversation, e)
                                }}
                              >
                                {t('history.delete')}
                              </button>
                            </div>
                          )}
                        </div>
                      </>
                    )}
                  </li>
                )
              })}
            </ul>
          </div>
        ))}
      </div>
    </aside>
  )

  if (mode === 'overlay') {
    return (
      <>
        <button
          type="button"
          className="absolute inset-0 z-20 bg-ink/25 animate-fade"
          aria-label={t('history.close')}
          onClick={onClose}
        />
        {panel}
      </>
    )
  }

  return panel
}
