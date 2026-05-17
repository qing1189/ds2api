import { RefreshCw, AlertTriangle, Activity, UserCheck, Loader2, Ban, ShieldAlert, ChevronDown, ChevronUp } from 'lucide-react'
import { useState } from 'react'
import clsx from 'clsx'

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

function successRate(state) {
    const total = state?.total_requests || 0
    if (total <= 0) return null
    const succ = state?.success_count || 0
    return Math.round((succ / total) * 100)
}

export default function WeightPanel({ weights, t, onReenable, reenableLoading }) {
    const [expanded, setExpanded] = useState(false)

    if (!weights) return null

    const disabled = weights.filter(w => w.disabled)
    const degraded = weights.filter(w => !w.disabled && w.current_weight < w.max_weight)
    const healthy = weights.filter(w => !w.disabled && w.current_weight >= w.max_weight)

    return (
        <div className="bg-card border border-border rounded-xl overflow-hidden shadow-sm">
            <div className="p-4 border-b border-border flex items-center justify-between">
                <div className="flex items-center gap-2">
                    <Activity className="w-4 h-4 text-muted-foreground" />
                    <h3 className="font-medium text-sm">{t('accountManager.weightTitle')}</h3>
                </div>
                {weights.length > 0 && (
                    <button
                        onClick={() => setExpanded(prev => !prev)}
                        className="flex items-center gap-1 text-[11px] text-muted-foreground hover:text-foreground transition-colors"
                    >
                        {t('accountManager.weightDetailTitle')}
                        {expanded ? <ChevronUp className="w-3 h-3" /> : <ChevronDown className="w-3 h-3" />}
                    </button>
                )}
            </div>

            {/* Summary cards */}
            <div className="grid grid-cols-3 gap-3 p-4 border-b border-border bg-muted/30">
                <div className="text-center">
                    <div className="text-xs text-muted-foreground mb-1">{t('accountManager.healthy')}</div>
                    <div className="text-lg font-bold text-emerald-500">{healthy.length}</div>
                </div>
                <div className="text-center">
                    <div className="text-xs text-muted-foreground mb-1">{t('accountManager.degraded')}</div>
                    <div className={clsx("text-lg font-bold", degraded.length > 0 ? "text-amber-500" : "text-muted-foreground")}>{degraded.length}</div>
                </div>
                <div className="text-center">
                    <div className="text-xs text-muted-foreground mb-1">{t('accountManager.disabled')}</div>
                    <div className={clsx("text-lg font-bold", disabled.length > 0 ? "text-red-500" : "text-muted-foreground")}>{disabled.length}</div>
                </div>
            </div>

            {/* Disabled accounts - show first as most important */}
            {disabled.length > 0 && (
                <div className="divide-y divide-border">
                    <div className="px-4 py-2 bg-red-500/5 border-b border-border">
                        <div className="flex items-center gap-1.5">
                            <AlertTriangle className="w-3.5 h-3.5 text-red-500" />
                            <span className="text-xs font-medium text-red-500">{t('accountManager.disabledAccounts')}</span>
                        </div>
                    </div>
                    {disabled.map((w, i) => {
                        const isManual = w.disable_reason === 'manual'
                        const reasonLabel = isManual
                            ? t('accountManager.disableReasonManual')
                            : t('accountManager.disableReasonAuto')
                        return (
                            <div key={i} className="px-4 py-2.5 flex items-center justify-between gap-2">
                                <div className="min-w-0">
                                    <div className="flex items-center gap-1.5">
                                        {isManual ? (
                                            <Ban className="w-3 h-3 text-red-500 shrink-0" />
                                        ) : (
                                            <ShieldAlert className="w-3 h-3 text-red-500 shrink-0" />
                                        )}
                                        <span className="text-xs font-medium truncate">{w.account_id}</span>
                                        <span className="px-1.5 py-0.5 text-[9px] font-medium bg-red-500/10 text-red-500 rounded shrink-0">
                                            {reasonLabel}
                                        </span>
                                    </div>
                                    <div className="text-[10px] text-muted-foreground mt-0.5 ml-4.5">
                                        {!isManual && t('accountManager.failCount', { count: w.consecutive_fails })}
                                        {w.disabled_at && ` · ${t('accountManager.disabledAt')}: ${formatTime(w.disabled_at)}`}
                                        {!isManual && w.last_failure_at && ` · ${t('accountManager.lastFail')}: ${formatTime(w.last_failure_at)}`}
                                    </div>
                                    {(w.total_requests || 0) > 0 && (
                                        <div className="text-[10px] text-muted-foreground mt-0.5 ml-4.5">
                                            {t('accountManager.weightStats', {
                                                succ: w.success_count || 0,
                                                fail: w.failure_count || 0,
                                                total: w.total_requests || 0,
                                            })}
                                        </div>
                                    )}
                                </div>
                                <button
                                    onClick={() => onReenable(w.account_id)}
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
                            </div>
                        )
                    })}
                </div>
            )}

            {/* Degraded accounts */}
            {degraded.length > 0 && (
                <div className="divide-y divide-border">
                    <div className="px-4 py-2 bg-amber-500/5 border-b border-border">
                        <div className="flex items-center gap-1.5">
                            <AlertTriangle className="w-3.5 h-3.5 text-amber-500" />
                            <span className="text-xs font-medium text-amber-500">{t('accountManager.degradedAccounts')}</span>
                        </div>
                    </div>
                    {degraded.map((w, i) => {
                        const rate = successRate(w)
                        return (
                            <div key={i} className="px-4 py-2 flex items-center justify-between gap-2">
                                <div className="min-w-0 flex-1">
                                    <div className="flex items-center gap-2">
                                        <span className="text-xs font-medium truncate">{w.account_id}</span>
                                        <span className="text-[10px] text-amber-500 font-medium shrink-0">
                                            {w.current_weight}/{w.max_weight}
                                        </span>
                                    </div>
                                    {/* Weight bar */}
                                    <div className="mt-1 h-1 bg-muted rounded-full overflow-hidden">
                                        <div
                                            className="h-full bg-amber-500 transition-all"
                                            style={{ width: `${(w.current_weight / Math.max(1, w.max_weight)) * 100}%` }}
                                        />
                                    </div>
                                    <div className="text-[10px] text-muted-foreground mt-1">
                                        {t('accountManager.degradeDetail', { fail: w.consecutive_fails, succ: w.consecutive_successes })}
                                        {(w.total_requests || 0) > 0 && (
                                            <>
                                                {' · '}
                                                {t('accountManager.weightStats', {
                                                    succ: w.success_count || 0,
                                                    fail: w.failure_count || 0,
                                                    total: w.total_requests || 0,
                                                })}
                                                {rate !== null && ` (${rate}%)`}
                                            </>
                                        )}
                                    </div>
                                </div>
                            </div>
                        )
                    })}
                </div>
            )}

            {/* Detailed list of all accounts (collapsible) */}
            {expanded && weights.length > 0 && (
                <div className="divide-y divide-border bg-muted/10">
                    <div className="px-4 py-2 border-b border-border">
                        <span className="text-xs font-medium text-muted-foreground">{t('accountManager.weightDetailTitle')}</span>
                    </div>
                    <div className="max-h-72 overflow-y-auto custom-scrollbar">
                        {weights.map((w, i) => {
                            const rate = successRate(w)
                            const stateColor = w.disabled
                                ? 'text-red-500'
                                : (w.current_weight < w.max_weight ? 'text-amber-500' : 'text-emerald-500')
                            return (
                                <div key={i} className="px-4 py-2 grid grid-cols-12 gap-2 items-center text-[11px] hover:bg-muted/30 transition-colors">
                                    <div className="col-span-4 truncate font-medium">{w.account_id}</div>
                                    <div className={clsx("col-span-2 font-mono", stateColor)}>
                                        {w.current_weight}/{w.max_weight}
                                    </div>
                                    <div className="col-span-3 text-muted-foreground truncate">
                                        {w.total_requests > 0
                                            ? `${w.success_count}/${w.failure_count}/${w.total_requests}`
                                            : '-'}
                                    </div>
                                    <div className="col-span-2 text-muted-foreground">
                                        {rate !== null ? `${rate}%` : '-'}
                                    </div>
                                    <div className="col-span-1 text-right text-muted-foreground truncate">
                                        {formatTime(w.last_success_at) || formatTime(w.last_failure_at) || '-'}
                                    </div>
                                </div>
                            )
                        })}
                    </div>
                </div>
            )}

            {/* Healthy accounts summary */}
            {healthy.length > 0 && !expanded && (
                <div className="px-4 py-2.5 flex items-center gap-2 text-[11px] text-muted-foreground">
                    <RefreshCw className="w-3 h-3" />
                    {t('accountManager.healthyCount', { count: healthy.length })}
                </div>
            )}

            {weights.length === 0 && (
                <div className="p-6 text-center text-xs text-muted-foreground">
                    {t('accountManager.noWeightData')}
                </div>
            )}
        </div>
    )
}
