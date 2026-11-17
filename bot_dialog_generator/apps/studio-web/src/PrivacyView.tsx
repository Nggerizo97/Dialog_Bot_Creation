import { useEffect, useRef, useState } from "react";
import { ShieldCheck } from "lucide-react";
import type { ErasureCategory, StaffRequest, StudioApi, SubjectRequest, WorkspaceAccess } from "./api";
import { Dialog } from "./Dialog";
import { legalHref } from "./legal";

const ACTION_LABEL: Record<ErasureCategory["action"], string> = {
  deleted: "deleted",
  retained: "kept",
  queued: "deletion requested from the service that holds it",
  not_stored: "not stored by the studio",
};

const CATEGORY_LABEL: Record<string, string> = {
  consent_records: "Consent records",
  conversation_sessions: "Conversation",
  conversation_analytics: "Conversation statistics",
  knowledge_references: "AI knowledge references",
  campaign_sends: "Campaign delivery records",
  campaign_suppression: "Do-not-contact entry",
  campaign_analytics: "Campaign statistics",
  workspace_access: "Access to areas",
  audit_log: "Audit log",
  authored_content: "Bots you built",
  identity_account: "Sign-in account",
};

const STATUS_LABEL: Record<string, string> = {
  pending: "Waiting for a platform administrator",
  completed: "Completed",
  partially_completed: "Partly completed",
};

export function ErasureResult({ result }: { result: ErasureCategory[] }) {
  if (result.length === 0) return null;
  return (
    <ul className="erasure-result">
      {result.map((c, i) => (
        <li key={`${c.category}-${i}`}>
          <strong>{CATEGORY_LABEL[c.category] ?? c.category}</strong>: {ACTION_LABEL[c.action]}
          {c.action === "deleted" ? ` (${c.count})` : ""}
          {c.detail ? `. ${c.detail}` : ""}
        </li>
      ))}
    </ul>
  );
}

const formatDate = (iso?: string) => (iso ? new Date(iso).toLocaleString() : "—");

/**
 * The right to erasure in the studio: anyone can ask for their own data to be deleted,
 * and owners delete the data of people who chatted with their area's bots or received
 * its campaigns, after checking the request is genuine.
 */
export function PrivacyView({
  api,
  workspace,
  onError,
  onStatus,
}: {
  api: StudioApi;
  workspace?: WorkspaceAccess;
  onError: (e: unknown) => void;
  onStatus: (msg: string) => void;
}) {
  const [mine, setMine] = useState<StaffRequest[]>([]);
  const [ledger, setLedger] = useState<SubjectRequest[]>([]);
  const [confirmMine, setConfirmMine] = useState(false);
  const [note, setNote] = useState("");
  const [kind, setKind] = useState<SubjectRequest["subject_kind"]>("chat_user");
  const [subjectId, setSubjectId] = useState("");
  const [verification, setVerification] = useState("");
  const [formError, setFormError] = useState("");
  const [confirmErase, setConfirmErase] = useState(false);
  const [lastErasure, setLastErasure] = useState<SubjectRequest | null>(null);
  const subjectField = useRef<HTMLInputElement>(null);
  const verificationField = useRef<HTMLTextAreaElement>(null);
  const isOwner = workspace?.role === "owner";

  useEffect(() => {
    api.listMyDeletionRequests().then(setMine).catch(onError);
  }, [api]);

  useEffect(() => {
    if (!workspace || !isOwner) return;
    api.listSubjectRequests(workspace.id).then(setLedger).catch(onError);
  }, [api, workspace?.id, isOwner]);

  const sendMyRequest = async () => {
    try {
      const req = await api.requestMyDeletion(note.trim());
      setMine((list) => [req, ...list.filter((r) => r.id !== req.id)]);
      setConfirmMine(false);
      setNote("");
      onStatus("Deletion request sent to the platform administrators");
    } catch (e) {
      onError(e);
    }
  };

  const reviewErasure = () => {
    if (!subjectId.trim()) {
      setFormError(kind === "email" ? "Enter the email address." : "Enter the conversation ID.");
      subjectField.current?.focus();
      return;
    }
    if (!verification.trim()) {
      setFormError("Say how you checked that the request comes from this person.");
      verificationField.current?.focus();
      return;
    }
    setFormError("");
    setConfirmErase(true);
  };

  const erase = async () => {
    if (!workspace) return;
    try {
      const req = await api.eraseSubject(workspace.id, { subject_kind: kind, subject_id: subjectId.trim(), verification: verification.trim() });
      setLedger((list) => [req, ...list]);
      setLastErasure(req);
      setSubjectId("");
      setVerification("");
      setConfirmErase(false);
      onStatus("The person's data was deleted");
    } catch (e) {
      setConfirmErase(false);
      onError(e);
    }
  };

  const openRequest = mine.find((r) => r.status !== "completed");

  return (
    <section className="workspace versions-view admin-view privacy-view">
      <h2>
        <ShieldCheck size={18} aria-hidden="true" /> Privacy
      </h2>

      <h3>Your data</h3>
      <p className="admin-note">
        The studio keeps your user ID, your access to each area, and an audit log of sensitive actions. See the{" "}
        <a href={legalHref("privacy")}>Privacy Policy</a> for what is kept and for how long.
      </p>
      {openRequest ? (
        <p className="admin-note" role="status">
          You asked for your data to be deleted on {formatDate(openRequest.created_at)}. Status: {STATUS_LABEL[openRequest.status]}.
        </p>
      ) : (
        <button type="button" className="test-button" onClick={() => setConfirmMine(true)}>
          Request deletion of my data
        </button>
      )}
      {mine.length > 0 && (
        <table className="versions-table">
          <caption className="sr-only">Your deletion requests</caption>
          <thead>
            <tr>
              <th scope="col">Requested</th>
              <th scope="col">Status</th>
              <th scope="col">Result</th>
            </tr>
          </thead>
          <tbody>
            {mine.map((r) => (
              <tr key={r.id}>
                <td>{formatDate(r.created_at)}</td>
                <td>{STATUS_LABEL[r.status] ?? r.status}</td>
                <td>
                  <ErasureResult result={r.result} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      {workspace && (
        <>
          <h3>People who used {workspace.name}'s bots</h3>
          {!isOwner ? (
            <p className="admin-note">Only owners of {workspace.name} can delete the data of people who chatted with its bots or received its campaigns.</p>
          ) : (
            <>
              <p className="admin-note">
                People can delete their own conversation from the chat widget. Use this form when someone asks by email or phone, after
                checking that the request really comes from them. The record below keeps only a one-way code, never the ID or address.
              </p>
              <form
                className="inline-form erasure-form"
                aria-label="Delete a person's data"
                noValidate
                onSubmit={(e) => {
                  e.preventDefault();
                  reviewErasure();
                }}
              >
                <label>
                  Identified by
                  <select value={kind} onChange={(e) => setKind(e.target.value as SubjectRequest["subject_kind"])}>
                    <option value="chat_user">Chat conversation ID</option>
                    <option value="email">Email address (campaigns)</option>
                  </select>
                </label>
                <label>
                  {kind === "email" ? "Email address" : "Conversation ID"}
                  <input
                    ref={subjectField}
                    value={subjectId}
                    onChange={(e) => setSubjectId(e.target.value)}
                    type={kind === "email" ? "email" : "text"}
                    autoComplete="off"
                    required
                    aria-required="true"
                    aria-describedby="subject-help"
                  />
                </label>
                <label className="wide">
                  How you checked it is their request (required)
                  <textarea
                    ref={verificationField}
                    value={verification}
                    onChange={(e) => setVerification(e.target.value)}
                    maxLength={500}
                    required
                    aria-required="true"
                  />
                </label>
                <p id="subject-help" className="field-help wide">
                  {kind === "email"
                    ? "Deletes the address's campaign records and makes sure it is never emailed again."
                    : "Deletes the conversation's consent records, and asks the conversation, statistics and AI services to delete their copies."}
                </p>
                {formError && (
                  <p className="form-error wide" role="alert">
                    {formError}
                  </p>
                )}
                <button type="submit" className="test-button">
                  Delete this person's data
                </button>
              </form>
              {lastErasure && (
                <div className="admin-note" role="status">
                  Deleted. Reference <code>{lastErasure.id}</code>.
                  <ErasureResult result={lastErasure.result} />
                </div>
              )}
              <table className="versions-table">
                <caption className="sr-only">Deletion requests for {workspace.name}</caption>
                <thead>
                  <tr>
                    <th scope="col">When</th>
                    <th scope="col">Person</th>
                    <th scope="col">From</th>
                    <th scope="col">Checked by</th>
                    <th scope="col">Result</th>
                  </tr>
                </thead>
                <tbody>
                  {ledger.length === 0 ? (
                    <tr>
                      <td colSpan={5}>No deletion requests yet.</td>
                    </tr>
                  ) : (
                    ledger.map((r) => (
                      <tr key={r.id}>
                        <td>{formatDate(r.created_at)}</td>
                        <td>
                          {r.subject_kind === "email" ? "Email" : "Chat"} <code title="One-way code, not the identifier">{r.subject_hash.slice(0, 12)}</code>
                        </td>
                        <td>{r.source === "webchat" ? "The person, in the chat" : "Studio"}</td>
                        <td>{r.requested_by}</td>
                        <td>
                          <ErasureResult result={r.result} />
                        </td>
                      </tr>
                    ))
                  )}
                </tbody>
              </table>
            </>
          )}
        </>
      )}

      {confirmMine && (
        <Dialog
          alert
          title="Request deletion of your data?"
          onClose={() => setConfirmMine(false)}
          description={
            <p>
              A platform administrator will remove your access to every area. Audit log entries are kept for the period in the Privacy Policy,
              and the bots you built stay with the organization. If you are an area's only owner, that access stays until someone else is
              made owner.
            </p>
          }
        >
          <label className="dialog-field">
            Note for the administrators (optional)
            <textarea value={note} onChange={(e) => setNote(e.target.value)} maxLength={1000} />
          </label>
          <div className="dialog-actions">
            <button type="button" className="choice-button" onClick={sendMyRequest}>
              Send request
            </button>
            <button type="button" className="choice-button" onClick={() => setConfirmMine(false)}>
              Cancel
            </button>
          </div>
        </Dialog>
      )}

      {confirmErase && (
        <Dialog
          alert
          title="Delete this person's data?"
          onClose={() => setConfirmErase(false)}
          description={<p>This cannot be undone. The deletion and the check you described are recorded in the audit log.</p>}
        >
          <div className="dialog-actions">
            <button type="button" className="choice-button" onClick={erase}>
              Delete data
            </button>
            <button type="button" className="choice-button" onClick={() => setConfirmErase(false)}>
              Cancel
            </button>
          </div>
        </Dialog>
      )}
    </section>
  );
}
