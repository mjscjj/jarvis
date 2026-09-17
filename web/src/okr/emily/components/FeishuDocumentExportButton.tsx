import { useEffect, useRef, useState } from 'react'
import { createFeishuDocument } from '../api'

export interface FeishuDocumentExport {
	title: string
	content: string
}

export function FeishuDocumentExportButton({
	label,
	document,
	disabled = false,
	resetKey = '',
	className = '',
}: {
	label: string
	document: () => FeishuDocumentExport
	disabled?: boolean
	resetKey?: string
	className?: string
}) {
	const [exporting, setExporting] = useState(false)
	const [result, setResult] = useState<{ url?: string; message?: string }>({})
	const requestID = useRef(0)

	useEffect(() => {
		requestID.current += 1
		setExporting(false)
		setResult({})
	}, [resetKey])

	const exportToFeishu = async () => {
		if (disabled || exporting) return
		const currentRequest = ++requestID.current
		setExporting(true)
		setResult({})
		try {
			const output = document()
			const saved = await createFeishuDocument(output.title, output.content)
			if (requestID.current !== currentRequest) return
			setResult({
				url: saved.url,
				message: saved.warnings.length > 0
					? `飞书文档已生成，另有 ${saved.warnings.length} 条转换提示。`
					: '飞书文档已生成。',
			})
		} catch (error) {
			if (requestID.current !== currentRequest) return
			setResult({ message: error instanceof Error ? error.message : '飞书文档生成失败。' })
		} finally {
			if (requestID.current === currentRequest) setExporting(false)
		}
	}

	return <>
		<button
			type="button"
			disabled={disabled || exporting}
			onClick={() => void exportToFeishu()}
			className={className}
		>{exporting ? '导出中…' : label}</button>
		{result.url && <a href={result.url} target="_blank" rel="noreferrer" className="text-[11px] font-medium text-blue-600 hover:text-blue-700 hover:underline">打开文档</a>}
		{result.message && <span className={`max-w-sm whitespace-normal text-[10px] ${result.url ? 'text-emerald-600' : 'text-red-500'}`} title={result.message}>{result.message}</span>}
	</>
}
