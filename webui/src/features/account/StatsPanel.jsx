import { useMemo, useState } from 'react'
import { BarChart3, KeyRound, RefreshCcw, Search, Users } from 'lucide-react'
import clsx from 'clsx'

const TAB_ACCOUNTS = 'accounts'
const TAB_KEYS = 'keys'

function formatNumber(n) {
    const v = Number(n || 0)
    if (!Number.isFinite(v)) return '0'
    if (v >= 1000000) return `${(v / 1000000).toFixed(1)}M`
    if (v >= 1000) return `${(v / 1000).toFixed(1)}k`
    return v.toLocaleString('en-US')
}

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

function successRate(row) {
    const total = Number(row?.total_requests || 0)
    if (total <= 0) return null
    const succ = Number(row?.success_count || 0)
    return Math.round((succ / total) * 100)
}

function aggregate(rows) {
    let total = 0, success = 0, failure = 0, input = 0, output = 0
    for (const r of rows || []) {
        total += Number(r.total_requests || 0)
        success += Number(r.success_count || 0)
        failure += Number(r.failure_count || 0)
        input += Number(r.input_tokens || 0)
        output += Number(r.output_tokens || 0)
    }
    return { total, success, failure, input, output }
}

export default function StatsPanel({ accountStats, apiKeyStats, t, onReset }) {
    const [tab, setTab] = useState(TAB_ACCOUNTS)
    const [query, setQuery] = useState('')
    const [resetting, setResetting] = useState(false)

    const accountAgg = useMemo(() => aggregate(accountStats), [accountStats])
    const keyAgg = useMemo(() => aggregate(apiKeyStats), [apiKeyStats])

    const isAccounts = tab === TAB_ACCOUNTS
    const rows = isAccounts ? (accountStats || []) : (apiKeyStats || [])
    const idField = isAccounts ? 'account_id' : 'api_key'

    const filtered = useMemo(() => {
        const q = query.trim().toLowerCase()
        const arr = q
            ? rows.filter(r => {
                const id = String(r[idField] || '').toLowerCase()
                const name = String(r.name || '').toLowerCase()
                const remark = String(r.remark || '').toLowerCase()
                return id.includes(q) || name.includes(q) || remark.includes(q)
            })
            : rows
        // Sort by total desc.
        return [...arr].sort((a, b) => Number(b.total_requests || 0) - Number(a.total_requests || 0))
    }, [rows, query, idField])

    const handleReset = async () => {
        if (!onReset) return
        if (!confirm(t('accountManager.statsResetConfirm'))) return
        setResetting(true)
        try {
            await onReset()
        } finally {
            setResetting(false)
        }
    }

    const totals = isAccounts ? accountAgg : keyAgg
    const emptyText = isAccounts
        ? t('accountManager.statsEmptyAccounts')
        : t('accountManager.statsEmptyKeys')

    return (
        <div className="bg-card border border-border rounded-xl overflow-hidden shadow-sm">
            <div className="p-4 border-b border-border flex items-center justify-between gap-3">
                <div className="flex items-center gap-2">
                    <BarChart3 className="w-4 h-4 text-muted-foreground" />
                    <h3 className="font-medium text-sm">{t('accountManager.statsTitle')}</h3>
                </div>
                <div className="flex items-center gap-2">
                    <div className="relative w-44 max-w-full">
                        <Search className="absolute left-2 top-1/2 -translate-y-1/2 w-3 h-3 text-muted-foreground" />
                        <input
                            type="text"
                            value={query}
                            onChange={e => setQuery(e.target.value)}
                            placeholder={t('accountManager.statsSearchPlaceholder')}
                            className="w-full pl-7 pr-2 py-1 text-xs bg-muted border border-border rounded-md focus:outline-none focus:ring-1 focus:ring-ring placeholder:text-muted-foreground"
                        />
                    </div>
                    <button
                        type="button"
                        onClick={handleReset}
                        disabled={resetting}
                        className="flex items-center gap-1 text-[11px] text-muted-foreground hover:text-foreground transition-colors disabled:opacity-50"
                    >
                        <RefreshCcw className={clsx('w-3 h-3', resetting && 'animate-spin')} />
                        {t('accountManager.statsReset')}
                    </button>
                </div>
            </div>

            {/* Tabs */}
            <div className="flex items-center border-b border-border bg-muted/20 overflow-x-auto">
                <TabButton
                    active={isAccounts}
                    icon={<Users className="w-3.5 h-3.5" />}
                    label={t('accountManager.statsTabAccounts')}
                    count={accountStats ? accountStats.length : 0}
                    onClick={() => setTab(TAB_ACCOUNTS)}
                />
                <TabButton
                    active={!isAccounts}
                    icon={<KeyRound className="w-3.5 h-3.5" />}
                    label={t('accountManager.statsTabKeys')}
                    count={apiKeyStats ? apiKeyStats.length : 0}
                    onClick={() => setTab(TAB_KEYS)}
                />
            </div>

            {/* Aggregate cards */}
            <div className="grid grid-cols-2 md:grid-cols-5 gap-3 p-4 border-b border-border bg-muted/30">
                <SummaryCard label={t('accountManager.statsTotalRequests')} value={formatNumber(totals.total)} />
                <SummaryCard label={t('accountManager.statsSuccessRequests')} value={formatNumber(totals.success)} color="text-emerald-500" />
                <SummaryCard label={t('accountManager.statsFailureRequests')} value={formatNumber(totals.failure)} color={totals.failure > 0 ? 'text-red-500' : 'text-muted-foreground'} />
                <SummaryCard label={t('accountManager.statsInputTokens')} value={formatNumber(totals.input)} color="text-sky-500" />
                <SummaryCard label={t('accountManager.statsOutputTokens')} value={formatNumber(totals.output)} color="text-violet-500" />
            </div>

            {/* Table */}
            {(rows.length === 0) ? (
                <div className="p-6 text-center text-xs text-muted-foreground">{emptyText}</div>
            ) : filtered.length === 0 ? (
                <div className="p-6 text-center text-xs text-muted-foreground">{t('accountManager.statsNoMatch')}</div>
            ) : (
                <div className="max-h-[480px] overflow-auto custom-scrollbar">
                    <table className="w-full text-xs">
                        <thead className="bg-muted/40 text-muted-foreground sticky top-0 z-10">
                            <tr>
                                <th className="text-left font-medium px-4 py-2">
                                    {isAccounts ? t('accountManager.statsAccountColumn') : t('accountManager.statsKeyColumn')}
                                </th>
                                <th className="text-right font-medium px-3 py-2 whitespace-nowrap">{t('accountManager.statsTotalColumn')}</th>
                                <th className="text-right font-medium px-3 py-2 whitespace-nowrap">{t('accountManager.statsSuccessColumn')}</th>
                                <th className="text-right font-medium px-3 py-2 whitespace-nowrap">{t('accountManager.statsFailureColumn')}</th>
                                <th className="text-right font-medium px-3 py-2 whitespace-nowrap">{t('accountManager.statsRateColumn')}</th>
                                <th className="text-right font-medium px-3 py-2 whitespace-nowrap">{t('accountManager.statsInputColumn')}</th>
                                <th className="text-right font-medium px-3 py-2 whitespace-nowrap">{t('accountManager.statsOutputColumn')}</th>
                                <th className="text-right font-medium px-3 py-2 whitespace-nowrap">{t('accountManager.statsLastColumn')}</th>
                            </tr>
                        </thead>
                        <tbody className="divide-y divide-border">
                            {filtered.map((row, i) => {
                                const id = String(row[idField] || '-')
                                const rate = successRate(row)
                                return (
                                    <tr key={`${id}-${i}`} className="hover:bg-muted/30 transition-colors">
                                        <td className="px-4 py-2 font-medium align-top">
                                            <div className="flex flex-col gap-0.5 min-w-0">
                                                <span className="truncate" title={id}>{id}</span>
                                                {(row.name || row.remark) && (
                                                    <span className="text-[10px] text-muted-foreground truncate">
                                                        {[row.name, row.remark].filter(Boolean).join(' · ')}
                                                    </span>
                                                )}
                                            </div>
                                        </td>
                                        <td className="text-right px-3 py-2 font-mono">{formatNumber(row.total_requests)}</td>
                                        <td className="text-right px-3 py-2 font-mono text-emerald-500">{formatNumber(row.success_count)}</td>
                                        <td className={clsx('text-right px-3 py-2 font-mono', Number(row.failure_count) > 0 ? 'text-red-500' : 'text-muted-foreground')}>
                                            {formatNumber(row.failure_count)}
                                        </td>
                                        <td className={clsx('text-right px-3 py-2 font-mono', rate === null ? 'text-muted-foreground' : rate >= 80 ? 'text-emerald-500' : rate >= 50 ? 'text-amber-500' : 'text-red-500')}>
                                            {rate === null ? '-' : `${rate}%`}
                                        </td>
                                        <td className="text-right px-3 py-2 font-mono text-sky-500">{formatNumber(row.input_tokens)}</td>
                                        <td className="text-right px-3 py-2 font-mono text-violet-500">{formatNumber(row.output_tokens)}</td>
                                        <td className="text-right px-3 py-2 text-muted-foreground whitespace-nowrap">
                                            {formatTime(row.last_request_at) || formatTime(row.last_success_at) || formatTime(row.last_failure_at) || '-'}
                                        </td>
                                    </tr>
                                )
                            })}
                        </tbody>
                    </table>
                </div>
            )}
        </div>
    )
}

function TabButton({ active, icon, label, count, onClick }) {
    return (
        <button
            onClick={onClick}
            className={clsx(
                'px-4 py-2.5 text-xs font-medium flex items-center gap-1.5 border-b-2 transition-colors whitespace-nowrap',
                active
                    ? 'border-primary text-foreground bg-background'
                    : 'border-transparent text-muted-foreground hover:text-foreground hover:bg-muted/50',
            )}
        >
            {icon}
            <span>{label}</span>
            <span className={clsx(
                'inline-flex items-center justify-center min-w-[1.25rem] px-1 h-4 text-[10px] font-mono rounded',
                active ? 'bg-muted' : 'bg-muted/50',
                'text-muted-foreground',
            )}>
                {count}
            </span>
        </button>
    )
}

function SummaryCard({ label, value, color }) {
    return (
        <div className="text-center">
            <div className="text-[10px] uppercase tracking-wide text-muted-foreground mb-1">{label}</div>
            <div className={clsx('text-base font-bold tabular-nums', color || 'text-foreground')}>{value}</div>
        </div>
    )
}
