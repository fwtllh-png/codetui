import {LoaderCircle, Trash2} from "lucide-react";
import {useId, useRef, useState} from "react";
import {createPortal} from "react-dom";
import type {RuntimeClient} from "../runtime/client";
import {IconButton} from "./primitives/IconButton";
import {Presence} from "./primitives/Presence";
import {useModalFocus} from "./primitives/useModalFocus";
import "./WorkspaceContextDialog.css";

export function TurnWithdrawalAction({client, turnID}: {
  client: RuntimeClient;
  turnID: string;
}) {
  const id = useId();
  const root = useRef<HTMLDivElement>(null);
  const inFlight = useRef(false);
  const [confirming, setConfirming] = useState(false);
  const [modalHost, setModalHost] = useState<HTMLElement | null>(null);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const close = () => {
    if (inFlight.current) return;
    setConfirming(false);
    setError("");
  };
  const withdraw = async () => {
    if (inFlight.current) return;
    inFlight.current = true;
    setPending(true);
    setError("");
    try {
      await client.withdrawTurn(turnID);
      setConfirming(false);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : String(reason));
    } finally {
      inFlight.current = false;
      setPending(false);
    }
  };
  return (
    <div ref={root} className="userMessageActions">
      <IconButton
        label="Withdraw turn"
        expanded={confirming}
        controls={confirming ? id : undefined}
        disabled={pending}
        icon={pending ? <LoaderCircle className="spin" size={14} /> : <Trash2 size={14} />}
        onClick={() => {
          setModalHost(root.current?.closest<HTMLElement>(".app") ?? document.body);
          setConfirming(true);
        }}
      />
      {modalHost && createPortal(
        <Presence open={confirming} kind="dialog">
          <WithdrawalConfirmation id={id} pending={pending} error={error}
            onCancel={close} onConfirm={() => void withdraw()} />
        </Presence>, modalHost
      )}
    </div>
  );
}

function WithdrawalConfirmation({id, pending, error, onCancel, onConfirm}: {
  id: string;
  pending: boolean;
  error: string;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const dialog = useRef<HTMLElement>(null);
  useModalFocus(dialog, true, onCancel);
  return (
    <div className="contextDialogOverlay" data-motion-backdrop onMouseDown={(event) => {
      if (event.target === event.currentTarget) onCancel();
    }}>
      <section ref={dialog} id={id} className="contextDialog confirmationDialog"
        data-motion-surface role="alertdialog" aria-modal="true" aria-busy={pending}
        aria-labelledby={`${id}-title`} aria-describedby={`${id}-description`} tabIndex={-1}>
        <header className="contextDialogHeader">
          <h2 id={`${id}-title`}>Withdraw this turn?</h2>
        </header>
        <p id={`${id}-description`} className="confirmationMessage">
          This turn will stop and be removed from model context.
          File changes and audit history will be kept.
        </p>
        {error && <p className="confirmationError" role="alert">{error}</p>}
        <div className="confirmationActions">
          <button type="button" disabled={pending} onClick={onCancel}>Cancel</button>
          <button type="button" className="confirmationDanger" disabled={pending} onClick={onConfirm}>
            {pending ? <LoaderCircle className="spin" size={14} /> : <Trash2 size={14} />}
            {pending ? "Withdrawing..." : "Withdraw"}
          </button>
        </div>
      </section>
    </div>
  );
}
