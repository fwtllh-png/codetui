import {parsePatch} from "diff";
import {Check, Copy} from "lucide-react";
import {useLayoutEffect, useMemo, useRef, useState} from "react";
import {IconButton} from "./primitives/IconButton";

type Row = {text: string; kind: string; before?: number; after?: number};

export function gitPatchRows(diff: string): Row[] {
  return parsePatch(diff).flatMap((file) => file.hunks.flatMap((hunk) => {
    let before = hunk.oldStart;
    let after = hunk.newStart;
    const rows: Row[] = [{
      text: `@@ -${hunk.oldStart},${hunk.oldLines} +${hunk.newStart},${hunk.newLines} @@`,
      kind: "hunk"
    }];
    for (const line of hunk.lines) {
      switch (line[0]) {
        case "+": rows.push({text: line, kind: "added", after: after++}); break;
        case "-": rows.push({text: line, kind: "removed", before: before++}); break;
        case " ": rows.push({text: line, kind: "context", before: before++, after: after++}); break;
        default: rows.push({text: line, kind: "meta"});
      }
    }
    return rows;
  }));
}

export function GitPatchView({diff}: {diff: string}) {
  const rows = useMemo(() => {
    try { return gitPatchRows(diff); } catch { return []; }
  }, [diff]);
  const ref = useRef<HTMLDivElement>(null);
  const [viewport, setViewport] = useState({top: 0, height: 0, row: 1});
  const [copied, setCopied] = useState(false);
  const [error, setError] = useState("");
  useLayoutEffect(() => {
    const node = ref.current;
    if (!node) return;
    node.scrollTop = 0;
    const measure = () => setViewport({
      top: node.scrollTop, height: node.clientHeight,
      row: parseFloat(getComputedStyle(node).lineHeight) || 1
    });
    measure();
    const observer = typeof ResizeObserver === "function" ? new ResizeObserver(measure) : undefined;
    observer?.observe(node);
    node.addEventListener("scroll", measure, {passive: true});
    return () => { observer?.disconnect(); node.removeEventListener("scroll", measure); };
  }, [diff]);
  const count = Math.max(1, Math.ceil(viewport.height / viewport.row));
  const start = Math.max(0, Math.floor(viewport.top / viewport.row) - count);
  const visible = rows.slice(start, start + count * 3);
  return <div className="gitPatchView">
    <div className="gitPatchActions">
      <IconButton label={copied ? "Diff copied" : "Copy Git diff"} icon={copied ? <Check size={14} /> : <Copy size={14} />}
        onClick={() => {
          void navigator.clipboard.writeText(diff).then(() => {setCopied(true); setError("");},
            () => setError("Clipboard unavailable"));
        }} />
    </div>
    {error && <p className="gitProblem" role="status">{error}</p>}
    {rows.length === 0 ? <pre>{diff}</pre> : <div ref={ref} className="gitPatchLines" tabIndex={0} role="region" aria-label="Diff lines">
      <div className="gitPatchSpacer" style={{height: rows.length * viewport.row}}>
        <div className="gitPatchWindow" style={{top: start * viewport.row}}>
          {visible.map((row, index) => <div className="gitPatchLine" data-kind={row.kind} key={start + index}>
            <span aria-hidden="true">{row.before}</span><span aria-hidden="true">{row.after}</span><code>{row.text}</code>
          </div>)}
        </div>
      </div>
    </div>}
  </div>;
}
