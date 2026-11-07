// The wire contract, mirroring internal/serve/view.go. The UI computes no status
// of its own: every string here is decided by the server (architecture D5).

export type CheckStatus = "pass" | "violated" | "unknown";

export type Verification = "verified" | "unknown";

export type Freshness = "updating" | "current" | "stale" | "failed";

export interface Ref {
  path: string;
  symbol: string;
  line_start: number;
  line_end: number;
  kind: string;
  provenance: string;
  confidence: number;
  verifier?: string;
}

export interface Check {
  id: string;
  subject: string;
  assertion: string;
  measures: "presence" | "dependency" | "test";
  status: CheckStatus;
  finding: string;
  reason?: string;
  evidence: Ref[];
  inspected: number;
  touched: boolean;
  verification: Verification;
  verification_evidence: Ref[] | null;
  spec_path: string;
  line: number;
}

export interface Goal {
  spec_path: string;
  heading: string[];
  label: string;
  text_hash: string;
  invariant: boolean;
  checks: Check[] | null;
  reading: string;
  verification: Verification;
  verification_evidence: Ref[] | null;
  diagnostics: string[] | null;
}

export interface DiffLine {
  kind: "added" | "removed";
  line: number;
  text: string;
}

export interface DiffFile {
  path: string;
  status: string;
  added: number;
  removed: number;
  lines: DiffLine[] | null;
  truncated: number;
}

export interface Baseline {
  state: "available" | "missing" | "conflicting";
  specs: string[] | null;
}

export interface Symbol {
  name: string;
  path: string;
  line: number;
  test?: boolean;
}

export interface Module {
  dir: string;
  files: string[];
  symbols: Symbol[];
  symbols_truncated?: boolean;
  depends_on: string[] | null;
  imports: string[] | null;
}

export interface Edge {
  from: string;
  to: string;
  kind?: "import" | "reference";
  import?: string;
  symbol?: string;
}

export interface Structure {
  modules: Module[];
  edges: Edge[];
  file_edges: Edge[];
}

export interface Conflict {
  spec_path: string;
  label: string;
  prompt: string;
  reason: string;
}

export interface Failure {
  at: string;
  reason: string;
}

export interface Cache {
  path: string;
  state: string;
}

export interface State {
  repository: string;
  commit?: string;
  generation?: string;
  freshness: Freshness;
  updated_at?: string;
  failure?: Failure;
  prompt_shown: boolean;
  baseline: Baseline;
  diff: { hash: string; files: DiffFile[] | null };
  structure: Structure;
  goals: Goal[] | null;
  prompt_conflicts: Conflict[] | null;
  diagnostics: string[] | null;
  cache: Cache;
}

export interface Envelope<T> {
  ok: boolean;
  data: T;
  error?: string;
}
