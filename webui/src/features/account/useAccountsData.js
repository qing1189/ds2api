import { useEffect, useState } from 'react'

export function useAccountsData({ apiFetch }) {
    const [queueStatus, setQueueStatus] = useState(null)
    const [weights, setWeights] = useState(null)
    const [accountStats, setAccountStats] = useState(null)
    const [apiKeyStats, setApiKeyStats] = useState(null)
    const [keysExpanded, setKeysExpanded] = useState(false)

    const [accounts, setAccounts] = useState([])
    const [page, setPage] = useState(1)
    const [pageSize, setPageSize] = useState(10)
    const [totalPages, setTotalPages] = useState(1)
    const [totalAccounts, setTotalAccounts] = useState(0)
    const [loadingAccounts, setLoadingAccounts] = useState(false)

    const resolveAccountIdentifier = (acc) => {
        if (!acc || typeof acc !== 'object') return ''
        return String(acc.identifier || acc.email || acc.mobile || '').trim()
    }

    const [searchQuery, setSearchQuery] = useState('')

    const fetchAccounts = async (targetPage = page, targetPageSize = pageSize, targetQuery = searchQuery) => {
        setLoadingAccounts(true)
        try {
            let url = `/admin/accounts?page=${targetPage}&page_size=${targetPageSize}`
            if (targetQuery.trim()) url += `&q=${encodeURIComponent(targetQuery.trim())}`
            const res = await apiFetch(url)
            if (res.ok) {
                const data = await res.json()
                setAccounts(data.items || [])
                setTotalPages(data.total_pages || 1)
                setTotalAccounts(data.total || 0)
                setPage(data.page || 1)
            }
        } catch (e) {
            console.error('Failed to fetch accounts:', e)
        } finally {
            setLoadingAccounts(false)
        }
    }

    const changePageSize = (newSize) => {
        setPageSize(newSize)
        fetchAccounts(1, newSize)
    }

    const handleSearchChange = (query) => {
        setSearchQuery(query)
        fetchAccounts(1, pageSize, query)
    }

    const fetchQueueStatus = async () => {
        try {
            const res = await apiFetch('/admin/queue/status')
            if (res.ok) {
                const data = await res.json()
                setQueueStatus(data)
            }
        } catch (e) {
            console.error('Failed to fetch queue status:', e)
        }
    }

    const fetchWeights = async () => {
        try {
            const res = await apiFetch('/admin/queue/weights')
            if (res.ok) {
                const data = await res.json()
                setWeights(data)
            }
        } catch (e) {
            console.error('Failed to fetch weights:', e)
        }
    }

    const fetchStats = async () => {
        try {
            const [accRes, keyRes] = await Promise.all([
                apiFetch('/admin/stats/accounts'),
                apiFetch('/admin/stats/keys'),
            ])
            if (accRes.ok) setAccountStats(await accRes.json())
            if (keyRes.ok) setApiKeyStats(await keyRes.json())
        } catch (e) {
            console.error('Failed to fetch stats:', e)
        }
    }

    const resetStats = async () => {
        try {
            const res = await apiFetch('/admin/stats/reset', { method: 'POST' })
            if (res.ok) {
                await fetchStats()
                return true
            }
        } catch (e) {
            console.error('Failed to reset stats:', e)
        }
        return false
    }

    useEffect(() => {
        fetchAccounts()
        fetchQueueStatus()
        fetchWeights()
        fetchStats()
        const interval = setInterval(() => {
            fetchQueueStatus()
            fetchWeights()
            fetchStats()
        }, 5000)
        return () => clearInterval(interval)
    }, [])

    return {
        queueStatus,
        weights,
        accountStats,
        apiKeyStats,
        keysExpanded,
        setKeysExpanded,
        accounts,
        page,
        pageSize,
        totalPages,
        totalAccounts,
        loadingAccounts,
        fetchAccounts,
        changePageSize,
        resolveAccountIdentifier,
        searchQuery,
        handleSearchChange,
        fetchStats,
        resetStats,
    }
}
