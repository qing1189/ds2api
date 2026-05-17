import { useMemo, useState } from 'react'
import { Activity, AlertTriangle, Ban, CheckCircle2, ChevronDown, ChevronUp, Loader2, Search, ShieldAlert, UserCheck } from 'lucide-react'
import clsx from 'clsx'

const TAB_ALL = 'all'
const TAB_HEALTHY = 'healthy'
const TAB_DEGRADED = 'degraded'
const TAB_DISABLED = 'disabled'

function formatTime(ts) {
    if (!ts) return null
    try {
        return new Date(ts).toLocaleString('zh-CN', {
            month: '2-digit',
            day: '2-digit',
            hour: '2-digit',
            minute: '2-digit',
        })
    } catch (_e) {
        return ts
    }
}

function getStatus(w) {
    if (!w) return TAB_HEALTHY
    if (w.disabled) return TAB_DISABLED
    if ((w.current_weight ?? 0) < (w.max_weight ?? 100)) return TAB_DEGRADED
    return TAB_HEALTHY
}

function successRate(state) {
    const total = state?.total_requests || 0
    if (total <= 0) return null
    const succ = state?.success_count || 0
    return Math.round((succ / total) * 100)
}

function StatusBadge({ status, reason, t }) {
    if (status === TAB_DISABLED) {
        const isManual = reason === 'manual'
        return (
            <span className="inline-flex items-center gap-1 px-1.5 py-0.5 text-[10px] font-medium bg-red-500/10 text-red-500 rounded">
                {isManual ? <Ban className="w-3 h-3" /> : <ShieldAlert className="w-3 h-3" />}
                {isManual ? t('accountManager.disableReasonManual') : t('accountManager.disableReasonAuto')}
            </span>
        )
    }
    if (status === TAB_DEGRADED) {
        return (
            <span className="inline-flex items-center gap-1 px-1.5 py-0.5 text-[10px] font-medium bg-amber-500/10 text-amber-500 rounded">
                <AlertTriangle className="w-3 h-3" />
                {t('accountManager.degraded')}
            </span>
        )
    }
    return (
        <span className="inline-flex items-center gap-1 px-1.5 py-0.5 text-[10px] font-medium bg-emerald-500/10 text-emerald-500 rounded">
            <CheckCircle2 className="w-3 h-3" />
            {t('accountManager.healthy')}
        </span>
    )
}

function WeightBar({ w }) {
    const max = Math.max(1, w.max_weight ?? 100)
    const cur = Math.max(0, w.current_weight ?? 0)
    const pct = (cur / max) * 100
    const color = w.disabled
        ? 'bg-red-500'
        : (cur < max ? 'bg-amber-500' : 'bg-emerald-500')
    return (
        <div className="h-1 bg-muted rounded-full overflow-hidden">
            <div className={clsx('h-full transition-all', color)} style={{ width: `${pct}%` }} />
        </div>
    )
}

function AccountRow({ w, t, onReenable, reenableLoading, expanded, onToggle }) {
    const status = getStatus(w)
    const rate = successRate(w)
    const isDisabled = status === TAB_DISABLED
    const total = w.total_requests || 0
    return (
        <div
            className={clsx(
                'transition-colors',
                isDisabled ? 'bg-red-500/[0.03]' : 'hover:bg-muted/30',
            )}
        >
            <div className="px-4 py-2.5 flex items-center justify-between gap-3">
                <button
                    type="button"
                    onClick={onToggle}
                    className="flex items-center gap-2 min-w-0 flex-1 text-left"
                >
                    <div className="shrink-0 w-2 h-2 rounded-full" style={{
                        background: status === TAB_DISABLED ? '#ef4444' : status === TAB_DEGRADED ? '#f59e0b' : '#10b981',
                        boxShadow: status === TAB_DISABLED ? '0 0 8px rgba(239,68,68,0.5)' : status === TAB_DEGRADED ? '0 0 8px rgba(245,158,11,0.5)' : '0 0 8px rgba(16,185,129,0.5)',
                    }} />
                    <span className="text-xs font-medium truncate">{w.account_id}</span>
                    <StatusBadge status={status} reason={w.disable_reason} t={t} />
                    <span className={clsx(
                        'shrink-0 font-mono text-[10px] px-1.5 py-0.5 rounded',
                        isDisabled ? 'bg-red-500/10 text-red-500' :
                        status === TAB_DEGRADED ? 'bg-amber-500/10 text-amber-500' :
                        'bg-emerald-500/10 text-emerald-500',
                    )}>
                        {w.current_weight}/{w.max_weight}
                    </span>
                    {expanded ? <ChevronUp className="w-3 h-3 text-muted-foreground shrink-0" /> : <ChevronDown className="w-3 h-3 text-muted-foreground shrink-0" />}
                </button>
                {isDisabled && (
                    <button
                        onClick={(e) => { e.stopPropagation(); onReenable(w.account_id) }}
                        disabled={reenableLoading && reenableLoading[w.account_id]}
                        className="flex items-center gap-1 shrink-0 px-2.5 py-1.5 text-[10px] font-medium bg-red-500/10 text-red-500 hover:bg-red-500/20 rounded-lg transition-colors disabled:opacity-50"
                    >
                        {reenableLoading && reenableLoading[w.account_id] ? (
                            <Loader2 className="w-3 h-3 animate-spin" />
                        ) : (
                            <UserCheck className="w-3 h-3" />
                        )}
                        {t('accountManager.reenable')}
                    </button>
                )}
            </div>
            <div className="px-4 pb-2">
                <WeightBar w={w} />
            </div>
            {expanded && (
                <div className="px-4 pb-3 grid grid-cols-2 md:grid-cols-4 gap-2 text-[10px]">
                    <div>
                        <div className="text-muted-foreground">{t('accountManager.successRate')}</div>
                        <div className="font-mono mt-0.5">{rate !== null ? `${rate}%` : '-'}</div>
                    </div>
                    <div>
                        <div className="text-muted-foreground">{t('accountManager.weightStats', { succ: w.success_count || 0, fail: w.failure_count || 0, total })}</div>
                        <div className="font-mono mt-0.5">
                            {total > 0 ? `${w.success_count || 0}/${w.failure_count || 0}/${total}` : '-'}
                        </div>
                    </div>
                    <div>
                        <div className="text-muted-foreground">{t('accountManager.lastSuccess')}</div>
                        <div className="font-mono mt-0.5">{formatTime(w.last_success_at) || '-'}</div>
                    </div>
                    <div>
                        <div className="text-muted-foreground">{t('accountManager.lastFail')}</div>
                        <div className="font-mono mt-0.5">{formatTime(w.last_failure_at) || '-'}</div>
                    </div>
                    {(status === TAB_DEGRADED || status === TAB_DISABLED) && (
                        <div className="col-span-2 md:col-span-4 text-muted-foreground">
                            {t('accountManager.degradeDetail', { fail: w.consecutive_fails || 0, succ: w.consecutive_successes || 0 })}
                            {isDisabled && w.disabled_at && ` · ${t('accountManager.disabledAt')}: ${formatTime(w.disabled_at)}`}
                        </div>
                    )}
                </div>
            )}
        </div>
    )
}

export default function WeightPanel({ weights, t, onReenable, reenableLoading }) {
    const [tab, setTab] = useState(TAB_ALL)
    const [query, setQuery] = useState('')
    const [openIds, setOpenIds] = useState(() => new Set())

    const list = Array.isArray(weights) ? weights : []

    const counts = useMemo(() => {
        const c = { all: list.length, healthy: 0, degraded: 0, disabled: 0 }
        for (const w of list) {
            const s = getStatus(w)
            if (s === TAB_DISABLED) c.disabled++
            else if (s === TAB_DEGRADED) c.degraded++
            else c.healthy++
        }
        return c
    }, [list])

    const filtered = useMemo(() => {
        const q = query.trim().toLowerCase()
        let arr = list
        if (tab !== TAB_ALL) arr = arr.filter(w => getStatus(w) === tab)
        if (q) arr = arr.filter(w => String(w.account_id || '').toLowerCase().includes(q))
        // sort: disabled first, then degraded, then healthy; within same group sort by account_id
        const order = { [TAB_DISABLED]: 0, [TAB_DEGRADED]: 1, [TAB_HEALTHY]: 2 }
        return [...arr].sort((a, b) => {
            const oa = order[getStatus(a)] ?? 9
            const ob = order[getStatus(b)] ?? 9
            if (oa !== ob) return oa - ob
            return String(a.account_id).localeCompare(String(b.account_id))
        })
    }, [list, tab, query])

    const toggleRow = (id) => {
        setOpenIds(prev => {
            const next = new Set(prev)
            if (next.has(id)) next.delete(id)
            else next.add(id)
            return next
        })
    }

    if (!weights) return null

    const tabs = [
        { key: TAB_ALL, label: t('accountManager.weightTabAll'), color: 'text-foreground', count: counts.all },
        { key: TAB_HEALTHY, label: t('accountManager.weightTabHealthy'), color: 'text-emerald-500', count: counts.healthy },
        { key: TAB_DEGRADED, label: t('accountManager.weightTabDegraded'), color: 'text-amber-500', count: counts.degraded },
        { key: TAB_DISABLED, label: t('accountManager.weightTabDisabled'), color: 'text-red-500', count: counts.disabled },
    ]

    const emptyText = tab === TAB_HEALTHY ? t('accountManager.weightEmptyHealthy')
        : tab === TAB_DEGRADED ? t('accountManager.weightEmptyDegraded')
        : tab === TAB_DISABLED ? t('accountManager.weightEmptyDisabled')
        : t('accountManager.noWeightData')

    return (
        <div className="bg-card border border-border rounded-xl overflow-hidden shadow-sm">
            <div className="p-4 border-b border-border flex items-center justify-between gap-3">
                <div className="flex items-center gap-2">
                    <Activity className="w-4 h-4 text-muted-foreground" />
                    <h3 className="font-medium text-sm">{t('accountManager.weightTitle')}</h3>
                </div>
                <div className="relative w-44 max-w-full">
                    <Search className="absolute left-2 top-1/2 -translate-y-1/2 w-3 h-3 text-muted-foreground" />
                    <input
                        type="text"
                        value={query}
                        onChange={e => setQuery(e.target.value)}
                        placeholder={t('accountManager.weightSearchPlaceholder')}
                        className="w-full pl-7 pr-2 py-1 text-xs bg-muted border border-border rounded-md focus:outline-none focus:ring-1 focus:ring-ring placeholder:text-muted-foreground"
                    />
                </div>
            </div>

            {/* Tabs */}
            <div className="flex items-center border-b border-border bg-muted/20 overflow-x-auto">
                {tabs.map(tb => {
                    const active = tab === tb.key
                    return (
                        <button
                            key={tb.key}
                            onClick={() => setTab(tb.key)}
                            className={clsx(
                                'px-4 py-2.5 text-xs font-medium flex items-center gap-1.5 border-b-2 transition-colors whitespace-nowrap',
                                active
                                    ? 'border-primary text-foreground bg-background'
                                    : 'border-transparent text-muted-foreground hover:text-foreground hover:bg-muted/50',
                            )}
                        >
                            <span className={active ? tb.color : ''}>{tb.label}</span>
                            <span className={clsx(
                                'inline-flex items-center justify-center min-w-[1.25rem] px-1 h-4 text-[10px] font-mono rounded',
                                active ? 'bg-muted' : 'bg-muted/50',
                                tb.count > 0 ? tb.color : 'text-muted-foreground',
                            )}>
                                {tb.count}
                            </span>
                        </button>
                    )
                })}
            </div>

            {/* List */}
            {filtered.length === 0 ? (
                <div className="p-6 text-center text-xs text-muted-foreground">{emptyText}</div>
            ) : (
                <div className="divide-y divide-border max-h-[480px] overflow-y-auto custom-scrollbar">
                    {filtered.map(w => (
                        <AccountRow
                            key={w.account_id}
                            w={w}
                            t={t}
                            onReenable={onReenable}
                            reenableLoading={reenableLoading}
                            expanded={openIds.has(w.account_id)}
                            onToggle={() => toggleRow(w.account_id)}
                        />
                    ))}
                </div>
            )}
        </div>
    )
}
