import React, { useEffect, useMemo, useRef, useState } from "react";

// A small Cmd/Ctrl-K command palette. `commands` is a flat list of
// { id, label, hint, run }. Fuzzy-ish substring filtering, arrow-key nav,
// Enter to run, Esc to close.
export default function CommandPalette({ open, onClose, commands }) {
  const [q, setQ] = useState("");
  const [active, setActive] = useState(0);
  const inputRef = useRef(null);

  const filtered = useMemo(() => {
    const t = q.trim().toLowerCase();
    if (!t) return commands;
    return commands.filter(
      (c) => c.label.toLowerCase().includes(t) || (c.hint || "").toLowerCase().includes(t)
    );
  }, [q, commands]);

  useEffect(() => {
    if (open) {
      setQ("");
      setActive(0);
      document.body.style.overflow = "hidden";
      // focus after paint
      const id = window.setTimeout(() => inputRef.current?.focus(), 20);
      return () => {
        window.clearTimeout(id);
        document.body.style.overflow = "";
      };
    }
  }, [open]);

  useEffect(() => { setActive(0); }, [q]);

  if (!open) return null;

  function choose(cmd) {
    if (!cmd) return;
    cmd.run();
  }

  function onKeyDown(e) {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setActive((a) => Math.min(a + 1, filtered.length - 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setActive((a) => Math.max(a - 1, 0));
    } else if (e.key === "Enter") {
      e.preventDefault();
      choose(filtered[active]);
    } else if (e.key === "Escape") {
      e.preventDefault();
      onClose();
    }
  }

  return (
    <div className="cmdk-backdrop" onClick={onClose}>
      <div className="cmdk" onClick={(e) => e.stopPropagation()}>
        <div className="cmdk-input-row">
          <span className="cmdk-prompt">&gt;_</span>
          <input
            ref={inputRef}
            className="cmdk-input"
            placeholder="Jump to a page or run a command…"
            value={q}
            onChange={(e) => setQ(e.target.value)}
            onKeyDown={onKeyDown}
          />
          <kbd className="cmdk-esc">esc</kbd>
        </div>
        <div className="cmdk-list">
          {filtered.length === 0 ? (
            <div className="cmdk-empty">No matches.</div>
          ) : (
            filtered.map((c, i) => (
              <div
                key={c.id}
                className={"cmdk-item" + (i === active ? " active" : "")}
                onMouseEnter={() => setActive(i)}
                onClick={() => choose(c)}
              >
                <span className="cmdk-label">{c.label}</span>
                {c.hint && <span className="cmdk-hint">{c.hint}</span>}
              </div>
            ))
          )}
        </div>
      </div>
    </div>
  );
}
