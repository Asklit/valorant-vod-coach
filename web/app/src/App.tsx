import {
  Activity,
  AlertTriangle,
  BarChart3,
  CheckCircle2,
  ChevronRight,
  Clock3,
  Crosshair,
  Database,
  FileJson2,
  FileText,
	FileVideo2,
  Gauge,
  History,
  Eye,
  Lightbulb,
  Link2,
  LogOut,
	Pencil,
  Play,
  Radar,
  RefreshCw,
  Search,
  Shield,
  ThumbsDown,
  ThumbsUp,
  Timer,
	Trash2,
	Upload,
  Video,
	X,
	XCircle
} from "lucide-react";
import type { FormEvent, ReactNode, RefObject } from "react";
import { useEffect, useMemo, useRef, useState } from "react";

type PageID = "dashboard" | "library" | "review" | "reports" | "admin";

type AuthUser = {
  id: string;
  email: string;
  display_name: string;
  role: "admin" | "user";
  created_at: string;
  last_login_at?: string;
};

type AuthResponse = {
  user: AuthUser;
  token: string;
};

type AuthSessionResponse = {
  authenticated: boolean;
  user?: AuthUser;
};

type VODItem = {
  label: string;
  rank: string;
  title: string;
  channel: string;
  video_id: string;
  source_url: string;
  duration_text: string;
  duration_seconds: number;
  rank_source: string;
  notes: string;
  enabled: boolean;
  local_status: string;
  local_size_bytes: number;
  video_url?: string;
  report_count: number;
  latest_report_id?: string;
  latest_generated?: string;
  latest_report_path?: string;
	map?: string;
	agent?: string;
	owner_id?: string;
	source_type: "curated" | "upload";
	uploaded_at?: string;
};

type UploadVODInput = {
	file: File;
	title: string;
	rank: string;
	map: string;
	agent: string;
};

type UploadVODResponse = { vod: VODItem };

type UpdateVODInput = Omit<UploadVODInput, "file">;

type VODListResponse = {
  generated_at: string;
  counts: {
    total: number;
    enabled: number;
    downloaded: number;
    reported: number;
  };
  vods: VODItem[];
};

type BackendHealth = {
  status: string;
  schema_version?: number;
  analyzer?: string;
  model_review_configured?: boolean;
  model_review_available?: boolean;
  vision_service?: {
    configured?: boolean;
    status?: string;
    model?: string;
    mode?: string;
    runtime?: string;
    error?: string;
  };
};

type Finding = {
  id: string;
  severity: string;
  category: string;
  title: string;
  detail: string;
  recommendation?: string;
  confidence?: number;
  evidence?: Array<{
    artifact_type: string;
    path: string;
    timestamp_seconds?: number;
    frame_index?: number;
  }>;
};

type Frame = {
  index: number;
  timestamp_seconds: number;
  path: string;
};

type ReviewWindow = {
  id: string;
  kind: string;
  severity: string;
  title: string;
  summary: string;
  recommendation: string;
  start_seconds: number;
  end_seconds: number;
  peak_seconds: number;
  round_number?: number;
  score: number;
  clip_path?: string;
  clip_duration_seconds?: number;
  evidence?: Finding["evidence"];
};

type CoachRecommendation = {
  summary: string;
  why_it_matters: string;
  better_action: string;
  drill: string;
  checkpoint: string;
};

type CoachQuestion = {
  id: string;
  prompt: string;
  required: boolean;
  options: Array<{ value: string; label: string }>;
};

type CoachDecision = {
  id: string;
  rule_id: string;
  window_id: string;
  kind: string;
  assessment: string;
  severity: string;
  title: string;
  observation: string;
  why_review: string;
  confidence: number;
  timestamp_seconds: number;
  start_seconds: number;
  end_seconds: number;
  round_number?: number;
  clip_path?: string;
  clip_duration_seconds?: number;
  evidence?: Finding["evidence"];
  requirements?: Array<{ id: string; label: string; status: string; detail?: string }>;
  questions?: CoachQuestion[];
  recommendation?: CoachRecommendation;
  tags?: string[];
};

type CoachReview = {
  schema_version: number;
  engine: string;
  method: string;
  status: string;
  summary: string;
  evidence_quality: {
    score: number;
    level: string;
    frame_coverage: number;
    hud_signal?: number;
    minimap_signal?: number;
    has_audio: boolean;
    macro_review_ready: boolean;
    micro_review_ready: boolean;
  };
  decisions: CoachDecision[];
  limitations?: string[];
};

type CoachSummary = {
  verdict: string;
  confidence: number;
  coverage_seconds?: number;
  focus_areas?: Array<{
    id: string;
    priority: string;
    category: string;
    title: string;
    detail: string;
    score: number;
    window_ids?: string[];
  }>;
  practice_plan?: Array<{
    id: string;
    title: string;
    detail: string;
    cadence: string;
  }>;
};

type PhaseStat = {
  phase: string;
  count: number;
  ratio: number;
};

type RoundSegment = {
  round_number: number;
  start_seconds: number;
  end_seconds: number;
  duration_seconds: number;
  detection_method: string;
  confidence: number;
  phase_profile?: PhaseStat[];
  review_window_ids?: string[];
  summary?: string;
};

type GameplayEvent = {
  id: string;
  type: string;
  category: string;
  severity: string;
  title: string;
  detail: string;
  recommendation?: string;
  timestamp_seconds: number;
  start_seconds?: number;
  end_seconds?: number;
  round_number?: number;
  score?: number;
  confidence?: number;
  evidence?: Finding["evidence"];
  window_id?: string;
};

type ModelReviewTask = {
  id: string;
  status: string;
  priority: string;
  prompt_version: string;
  model_hint?: string;
  window_id: string;
  round_number?: number;
  kind: string;
  severity: string;
  clip_path?: string;
  clip_duration_seconds?: number;
  start_seconds: number;
  end_seconds: number;
  peak_seconds: number;
  prompt: string;
};

type GameplaySummary = {
  analyzer?: string;
  sampled_frames: number;
  analyzed_frames: number;
  skipped_frames?: number;
  review_window_count: number;
  round_segment_count?: number;
  model_review_task_count?: number;
  model_review_run_count?: number;
  average_motion_score?: number;
  average_minimap_signal?: number;
  average_hud_signal?: number;
  peak_combat_score?: number;
  coach?: CoachSummary;
  coach_review?: CoachReview;
  phase_profile?: PhaseStat[];
  round_segments?: RoundSegment[];
  gameplay_events?: GameplayEvent[];
  model_review_tasks?: ModelReviewTask[];
  review_windows?: ReviewWindow[];
};

type Report = {
  schema_version: number;
  run_id: string;
  status: string;
  generated_at: string;
  vod: {
    label: string;
    rank: string;
    title: string;
    channel: string;
    source_url: string;
  };
  media: {
    duration_seconds?: number;
    has_duration: boolean;
    size_bytes?: number;
    has_size: boolean;
    video_codec?: string;
    width?: number;
    height?: number;
    frame_rate?: string;
    audio_codec?: string;
    has_audio: boolean;
  };
  sample: {
    name: string;
    manifest_path: string;
    fps: string;
    start_seconds: number;
    duration_seconds?: number;
    frame_count: number;
    frames?: Frame[];
    contact_sheet_path?: string;
  };
  gameplay?: GameplaySummary;
  findings: Finding[];
  timeline: Array<{
    timestamp_seconds: number;
    type: string;
    title: string;
    detail?: string;
  }>;
  artifacts: Array<{
    type: string;
    format: string;
    path: string;
  }>;
  metadata: {
    analyzer: string;
    coach_engine?: string;
    mode: string;
  };
};

type AnalysisJobResponse = {
  job_id: string;
  run_id: string;
  vod_label: string;
  status: "queued" | "running" | "completed" | "failed";
  message?: string;
  created_at: string;
  started_at?: string;
  finished_at?: string;
  error?: string;
  report?: Report;
  report_json?: string;
  report_md?: string;
};

type ReportSummary = {
  schema_version: number;
  run_id: string;
  status: string;
  generated_at: string;
  finding_count: number;
  frame_count: number;
  review_window_count: number;
  round_segment_count: number;
  model_review_task_count: number;
  model_review_run_count: number;
  analyzer?: string;
  sample_name: string;
  sample_fps: string;
  sample_duration_seconds?: number;
  contact_sheet?: string;
  json_path: string;
  markdown_path: string;
};

type ReportListResponse = {
  vod_label: string;
  reports: ReportSummary[];
};

type EvaluationSummary = {
  schema_version: number;
  run_id: string;
  generated_at: string;
  vod_label: string;
  report_run_id: string;
  tolerance_seconds: number;
  label_count: number;
  prediction_count: number;
  match_count: number;
  precision: number;
  recall: number;
  f1: number;
  json_path: string;
  markdown_path: string;
};

type EvaluationListResponse = {
  vod_label: string;
  evaluations: EvaluationSummary[];
};

type EvaluationAnnotationSummary = {
  schema_version: number;
  vod_label: string;
  report_run_id?: string;
  tolerance_seconds?: number;
  label_count: number;
  path: string;
};

type EvaluationAnnotationListResponse = {
  vod_label: string;
  annotations: EvaluationAnnotationSummary[];
};

type ManualCorrection = {
  id: string;
  type: string;
  target_id?: string;
  corrected_value?: string;
  comment?: string;
  timestamp_seconds?: number;
  status: string;
  author?: string;
  created_at: string;
};

type ManualCorrectionResponse = {
  vod_label: string;
  report_run_id?: string;
  corrections: ManualCorrection[];
  json_path: string;
};

type GuidedReviewAssessment = {
  id: string;
  decision_id: string;
  window_id: string;
  answers: Record<string, string>;
  result: CoachDecision;
  author?: string;
  created_at: string;
  updated_at: string;
  feedback?: { verdict: string; comment?: string; author?: string; updated_at: string };
};

type CoachReviewProgress = {
  total: number;
  completed: number;
  actionable: number;
  neutral: number;
  not_applicable: number;
};

type CoachAssessmentsResponse = {
  vod_label: string;
  report_run_id: string;
  progress: CoachReviewProgress;
  assessments: GuidedReviewAssessment[];
  json_path: string;
};

type AdminOverview = {
  generated_at: string;
  user: AuthUser;
  system: {
    schema_version: number;
    analyzer: string;
    model_review_enabled: boolean;
    manifest_path: string;
    raw_root: string;
    processed_root: string;
    evaluation_label_root: string;
  };
  readiness: Record<string, ReadinessCheck>;
  dataset: VODListResponse["counts"];
  jobs: Record<string, number>;
  auth: {
    user_count: number;
  };
};

type AdminMetric = {
  method: string;
  route: string;
  status: number;
  count: number;
  duration_seconds: number;
};

type ReadinessCheck = {
  status: string;
  detail?: string;
  path?: string;
  runtime?: string;
  model?: string;
};

type RequestLog = {
  time: string;
  method: string;
  path: string;
  route: string;
  status: number;
  duration_ms: number;
  user_id?: string;
  user_email?: string;
  user_role?: string;
};

type AdminMetricsResponse = {
  started_at: string;
  requests: AdminMetric[];
  jobs: Record<string, number>;
  logs: RequestLog[];
  routes: string[];
  user: AuthUser;
};

type AdminLogsResponse = {
  logs: RequestLog[];
};

type AdminUsersResponse = {
  users: AuthUser[];
};

const ranks = ["all", "iron", "bronze", "silver", "gold", "platinum", "diamond", "ascendant", "immortal", "radiant"];
const correctionTypes = ["false_detection", "map", "agent", "rank", "round_boundary", "finding_note", "event_note"];
const authStorageKey = "vodcoach.auth";

export function App() {
  const [page, setPage] = useState<PageID>("dashboard");
  const [token, setToken] = useState(() => readStoredAuth()?.token ?? "");
  const [user, setUser] = useState<AuthUser | null>(() => readStoredAuth()?.user ?? null);
  const [authMode, setAuthMode] = useState<"login" | "register">("login");
  const [authEmail, setAuthEmail] = useState("");
  const [authPassword, setAuthPassword] = useState("");
  const [authName, setAuthName] = useState("");

  const [vods, setVods] = useState<VODItem[]>([]);
  const [counts, setCounts] = useState<VODListResponse["counts"] | null>(null);
  const [backendHealth, setBackendHealth] = useState<BackendHealth | null>(null);
  const [selectedLabel, setSelectedLabel] = useState("");
  const [rank, setRank] = useState("all");
  const [query, setQuery] = useState("");
  const [report, setReport] = useState<Report | null>(null);
  const [reportHistory, setReportHistory] = useState<ReportSummary[]>([]);
  const [evaluationHistory, setEvaluationHistory] = useState<EvaluationSummary[]>([]);
  const [evaluationAnnotations, setEvaluationAnnotations] = useState<EvaluationAnnotationSummary[]>([]);
  const [manualCorrections, setManualCorrections] = useState<ManualCorrection[]>([]);
  const [manualCorrectionsPath, setManualCorrectionsPath] = useState("");
	const [coachAssessments, setCoachAssessments] = useState<GuidedReviewAssessment[]>([]);
	const [coachProgress, setCoachProgress] = useState<CoachReviewProgress | null>(null);
	const [coachAssessmentsPath, setCoachAssessmentsPath] = useState("");
  const [analysisJob, setAnalysisJob] = useState<AnalysisJobResponse | null>(null);
  const [adminOverview, setAdminOverview] = useState<AdminOverview | null>(null);
  const [adminMetrics, setAdminMetrics] = useState<AdminMetricsResponse | null>(null);
  const [adminLogs, setAdminLogs] = useState<RequestLog[]>([]);
  const [adminUsers, setAdminUsers] = useState<AuthUser[]>([]);

  const [loading, setLoading] = useState(true);
  const [loadingReport, setLoadingReport] = useState(false);
  const [analyzing, setAnalyzing] = useState(false);
  const [evaluating, setEvaluating] = useState(false);
  const [savingCorrection, setSavingCorrection] = useState(false);
  const [savingCoachReview, setSavingCoachReview] = useState(false);
	const [uploadingVod, setUploadingVod] = useState(false);
	const [uploadProgress, setUploadProgress] = useState(0);
	const [mutatingVod, setMutatingVod] = useState("");
  const [error, setError] = useState("");
  const [runDuration, setRunDuration] = useState(180);
  const [runFps, setRunFps] = useState("1");
  const [fullVod, setFullVod] = useState(false);
  const [correctionType, setCorrectionType] = useState("false_detection");
  const [correctionTargetID, setCorrectionTargetID] = useState("");
  const [correctionValue, setCorrectionValue] = useState("");
  const [correctionComment, setCorrectionComment] = useState("");
  const videoRef = useRef<HTMLVideoElement | null>(null);

  const authHeaders = useMemo<Record<string, string>>(() => {
    const headers: Record<string, string> = {};
    if (!token) {
      return headers;
    }
    headers.Authorization = `Bearer ${token}`;
    return headers;
  }, [token]);
  const jsonHeaders = useMemo<Record<string, string>>(() => ({ ...authHeaders, "Content-Type": "application/json" }), [authHeaders]);
  const selectedVod = useMemo(() => vods.find((vod) => vod.label === selectedLabel) ?? null, [selectedLabel, vods]);
  const filteredVods = useMemo(() => {
    return vods.filter((vod) => {
      const rankOk = rank === "all" || vod.rank === rank;
      const text = `${vod.label} ${vod.rank} ${vod.title} ${vod.channel}`.toLowerCase();
      const queryOk = !query.trim() || text.includes(query.trim().toLowerCase());
      return rankOk && queryOk;
    });
  }, [query, rank, vods]);
  const latestReportSummary = reportHistory.find((item) => item.run_id === report?.run_id) ?? reportHistory[0] ?? null;
  const correctionTargets = useMemo(() => buildCorrectionTargets(report), [report]);

  useEffect(() => {
    void loadSession();
  }, []);

  useEffect(() => {
    if (!user) {
      return;
    }
    void loadBootstrap();
  }, [user?.id]);

  useEffect(() => {
    if (!selectedLabel) {
      setReport(null);
      setReportHistory([]);
      setEvaluationHistory([]);
      setEvaluationAnnotations([]);
      return;
    }
    void loadReports(selectedLabel, { preferGameplay: true });
    void loadEvaluations(selectedLabel);
    void loadEvaluationAnnotations(selectedLabel);
  }, [selectedLabel]);

  useEffect(() => {
    setCorrectionTargetID("");
    setCorrectionValue("");
    setCorrectionComment("");
    if (!selectedLabel || !report?.run_id) {
      setManualCorrections([]);
      setManualCorrectionsPath("");
		setCoachAssessments([]);
		setCoachProgress(null);
		setCoachAssessmentsPath("");
      return;
    }
    void loadManualCorrections(selectedLabel, report.run_id);
	void loadCoachAssessments(selectedLabel, report.run_id);
  }, [selectedLabel, report?.run_id]);

  useEffect(() => {
    if (page === "admin" && user?.role === "admin") {
      void loadAdmin();
    }
  }, [page, user?.role]);

  async function loadSession() {
    try {
      const response = await apiFetch("/api/auth/session", { headers: authHeaders });
      if (!response.ok) {
        throw new Error(await readError(response));
      }
      const payload = (await response.json()) as AuthSessionResponse;
      if (!payload.authenticated || !payload.user) {
        clearAuth();
        return;
      }
      setUser(payload.user);
    } catch {
      clearAuth();
    } finally {
      setLoading(false);
    }
  }

  async function submitAuth() {
    setError("");
    try {
      const path = authMode === "login" ? "/api/auth/login" : "/api/auth/register";
      const response = await apiFetch(path, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email: authEmail, password: authPassword, display_name: authName })
      });
      if (!response.ok) {
        throw new Error(await readError(response));
      }
      const payload = (await response.json()) as AuthResponse;
      setToken(payload.token);
      setUser(payload.user);
      window.localStorage.setItem(authStorageKey, JSON.stringify(payload));
    } catch (err) {
      setError(messageFromError(err));
    }
  }

  async function logout() {
    await apiFetch("/api/auth/logout", { method: "POST", headers: authHeaders }).catch(() => undefined);
    clearAuth();
  }

  function clearAuth() {
    setToken("");
    setUser(null);
    window.localStorage.removeItem(authStorageKey);
  }

  async function loadBootstrap() {
    await Promise.all([loadBackendHealth(), loadVods()]);
  }

  async function loadBackendHealth() {
    try {
      const response = await apiFetch("/api/health");
      if (response.ok) {
        setBackendHealth((await response.json()) as BackendHealth);
      }
    } catch {
      setBackendHealth(null);
    }
  }

  async function loadVods() {
    setLoading(true);
    setError("");
    try {
      const response = await apiFetch("/api/vods", { headers: authHeaders });
      if (!response.ok) {
        throw new Error(await readError(response));
      }
      const payload = (await response.json()) as VODListResponse;
      setVods(payload.vods);
      setCounts(payload.counts);
      setSelectedLabel((current) => current || payload.vods.find((vod) => vod.report_count > 0)?.label || payload.vods[0]?.label || "");
    } catch (err) {
      setError(messageFromError(err));
    } finally {
      setLoading(false);
    }
  }

  async function loadReports(label: string, options: { preferredRunID?: string; preferGameplay?: boolean } = {}) {
    setLoadingReport(true);
    try {
      const response = await apiFetch(`/api/reports?vod_label=${encodeURIComponent(label)}`, { headers: authHeaders });
      if (!response.ok) {
        throw new Error(await readError(response));
      }
      const payload = (await response.json()) as ReportListResponse;
      setReportHistory(payload.reports);
      if (payload.reports.length === 0) {
        setReport(null);
        return;
      }
      const preferred =
        payload.reports.find((item) => item.run_id === options.preferredRunID) ??
        (options.preferGameplay ? payload.reports.find((item) => item.review_window_count > 0) : undefined) ??
        payload.reports[0];
      await loadReport(label, preferred.run_id);
    } catch (err) {
      setError(messageFromError(err));
      setReport(null);
      setReportHistory([]);
    } finally {
      setLoadingReport(false);
    }
  }

  async function loadReport(label: string, runID: string) {
    setLoadingReport(true);
    try {
      const response = await apiFetch(`/api/reports/${encodeURIComponent(label)}/${encodeURIComponent(runID)}`, { headers: authHeaders });
      if (!response.ok) {
        throw new Error(await readError(response));
      }
      setReport((await response.json()) as Report);
    } catch (err) {
      setError(messageFromError(err));
      setReport(null);
    } finally {
      setLoadingReport(false);
    }
  }

  async function loadEvaluations(label: string) {
    try {
      const response = await apiFetch(`/api/evaluations?vod_label=${encodeURIComponent(label)}`, { headers: authHeaders });
      if (response.ok) {
        setEvaluationHistory(((await response.json()) as EvaluationListResponse).evaluations);
      }
    } catch {
      setEvaluationHistory([]);
    }
  }

  async function loadEvaluationAnnotations(label: string) {
    try {
      const response = await apiFetch(`/api/evaluation-annotations?vod_label=${encodeURIComponent(label)}`, { headers: authHeaders });
      if (response.ok) {
        setEvaluationAnnotations(((await response.json()) as EvaluationAnnotationListResponse).annotations);
      }
    } catch {
      setEvaluationAnnotations([]);
    }
  }

  async function loadManualCorrections(label: string, runID: string) {
    try {
      const response = await apiFetch(`/api/corrections?vod_label=${encodeURIComponent(label)}&report_run_id=${encodeURIComponent(runID)}`, { headers: authHeaders });
      if (response.ok) {
        const payload = (await response.json()) as ManualCorrectionResponse;
        setManualCorrections(payload.corrections);
        setManualCorrectionsPath(payload.json_path);
      }
    } catch {
      setManualCorrections([]);
      setManualCorrectionsPath("");
    }
  }

	async function loadCoachAssessments(label: string, runID: string) {
		try {
			const response = await apiFetch(`/api/coach-assessments?vod_label=${encodeURIComponent(label)}&report_run_id=${encodeURIComponent(runID)}`, { headers: authHeaders });
			if (!response.ok) { throw new Error(await readError(response)); }
			const payload = (await response.json()) as CoachAssessmentsResponse;
			setCoachAssessments(payload.assessments);
			setCoachProgress(payload.progress);
			setCoachAssessmentsPath(payload.json_path);
		} catch {
			setCoachAssessments([]);
			setCoachProgress(null);
			setCoachAssessmentsPath("");
		}
	}

  async function loadAdmin() {
    try {
      const [overview, metrics, logs, users] = await Promise.all([
        fetchJSON<AdminOverview>("/api/admin/overview"),
        fetchJSON<AdminMetricsResponse>("/api/admin/metrics"),
        fetchJSON<AdminLogsResponse>("/api/admin/logs"),
        fetchJSON<AdminUsersResponse>("/api/admin/users")
      ]);
      setAdminOverview(overview);
      setAdminMetrics(metrics);
      setAdminLogs(logs.logs);
      setAdminUsers(users.users);
    } catch (err) {
      setError(messageFromError(err));
    }
  }

  async function fetchJSON<T>(path: string): Promise<T> {
    const response = await apiFetch(path, { headers: authHeaders });
    if (!response.ok) {
      throw new Error(await readError(response));
    }
    return (await response.json()) as T;
  }

  async function runAnalysis() {
    if (!selectedVod || analyzing) {
      return;
    }
    setAnalyzing(true);
    setAnalysisJob(null);
    setError("");
    try {
      const response = await apiFetch("/api/analysis-runs", {
        method: "POST",
        headers: jsonHeaders,
        body: JSON.stringify({
          vod_label: selectedVod.label,
          run_id: `ui_${compactTimestamp(new Date())}`,
          fps: runFps,
          image_quality: 3,
          duration_seconds: fullVod ? 0 : runDuration,
          force: true,
		  model_review: false,
          async: true
        })
      });
      if (!response.ok) {
        throw new Error(await readError(response));
      }
      const payload = (await response.json()) as AnalysisJobResponse;
      setAnalysisJob(payload);
      await pollAnalysisJob(payload.job_id, selectedVod.label);
    } catch (err) {
      setError(messageFromError(err));
    } finally {
      setAnalyzing(false);
    }
  }

  async function pollAnalysisJob(jobID: string, analyzedLabel: string) {
    for (;;) {
      await sleep(1600);
      const response = await apiFetch(`/api/analysis-runs/${encodeURIComponent(jobID)}`, { headers: authHeaders });
      if (!response.ok) {
        throw new Error(await readError(response));
      }
      const job = (await response.json()) as AnalysisJobResponse;
      setAnalysisJob(job);
      if (job.status === "completed") {
        await loadVods();
        await loadReports(analyzedLabel, { preferredRunID: job.run_id });
        await loadEvaluations(analyzedLabel);
        setPage("review");
        return;
      }
      if (job.status === "failed") {
        throw new Error(job.error || "Analysis failed");
      }
    }
  }

  async function runEvaluation() {
    if (!selectedVod || !report || evaluating || evaluationAnnotations.length === 0) {
      return;
    }
    const annotation = evaluationAnnotations.find((item) => item.report_run_id === report.run_id) ?? evaluationAnnotations[0];
    setEvaluating(true);
    setError("");
    try {
      const response = await apiFetch("/api/evaluation-runs", {
        method: "POST",
        headers: jsonHeaders,
        body: JSON.stringify({
          vod_label: selectedVod.label,
          report_run_id: report.run_id,
          annotations_path: annotation.path,
          run_id: `ui_eval_${compactTimestamp(new Date())}`,
          tolerance_seconds: annotation.tolerance_seconds ?? 0,
          force: true
        })
      });
      if (!response.ok) {
        throw new Error(await readError(response));
      }
      await loadEvaluations(selectedVod.label);
    } catch (err) {
      setError(messageFromError(err));
    } finally {
      setEvaluating(false);
    }
  }

  async function saveManualCorrection() {
    if (!selectedVod || !report || savingCorrection) {
      return;
    }
    if (!correctionValue.trim() && !correctionComment.trim()) {
      setError("Correction value or comment is required.");
      return;
    }
    setSavingCorrection(true);
    setError("");
    try {
      const currentTime = videoRef.current?.currentTime;
      const response = await apiFetch("/api/corrections", {
        method: "POST",
        headers: jsonHeaders,
        body: JSON.stringify({
          vod_label: selectedVod.label,
          report_run_id: report.run_id,
          type: correctionType,
          target_id: correctionTargetID,
          corrected_value: correctionValue,
          comment: correctionComment,
          timestamp_seconds: Number.isFinite(currentTime) ? currentTime : undefined
        })
      });
      if (!response.ok) {
        throw new Error(await readError(response));
      }
      const payload = (await response.json()) as ManualCorrectionResponse;
      setManualCorrections(payload.corrections);
      setManualCorrectionsPath(payload.json_path);
      setCorrectionValue("");
      setCorrectionComment("");
    } catch (err) {
      setError(messageFromError(err));
    } finally {
      setSavingCorrection(false);
    }
  }

	async function saveCoachAssessment(windowID: string, answers: Record<string, string>) {
		if (!selectedVod || !report || savingCoachReview) { return; }
		setSavingCoachReview(true);
		setError("");
		try {
			const response = await apiFetch("/api/coach-assessments", {
				method: "POST", headers: jsonHeaders,
				body: JSON.stringify({ vod_label: selectedVod.label, report_run_id: report.run_id, window_id: windowID, answers })
			});
			if (!response.ok) { throw new Error(await readError(response)); }
			const payload = (await response.json()) as CoachAssessmentsResponse;
			setCoachAssessments(payload.assessments);
			setCoachProgress(payload.progress);
			setCoachAssessmentsPath(payload.json_path);
		} catch (err) {
			setError(messageFromError(err));
		} finally {
			setSavingCoachReview(false);
		}
	}

	async function rateCoachRecommendation(windowID: string, verdict: string) {
		if (!selectedVod || !report) { return; }
		setError("");
		try {
			const response = await apiFetch("/api/coach-feedback", {
				method: "POST", headers: jsonHeaders,
				body: JSON.stringify({ vod_label: selectedVod.label, report_run_id: report.run_id, window_id: windowID, verdict })
			});
			if (!response.ok) { throw new Error(await readError(response)); }
			const payload = (await response.json()) as CoachAssessmentsResponse;
			setCoachAssessments(payload.assessments);
			setCoachProgress(payload.progress);
		} catch (err) {
			setError(messageFromError(err));
		}
	}

	async function uploadVOD(input: UploadVODInput): Promise<boolean> {
		if (uploadingVod) { return false; }
		setUploadingVod(true);
		setUploadProgress(0);
		setError("");
		try {
			const form = new FormData();
			form.append("title", input.title);
			form.append("rank", input.rank);
			form.append("map", input.map);
			form.append("agent", input.agent);
			form.append("file", input.file, input.file.name);
			const payload = await new Promise<UploadVODResponse>((resolve, reject) => {
				const request = new XMLHttpRequest();
				request.open("POST", apiURL("/api/vods/upload"));
				request.withCredentials = true;
				if (token) { request.setRequestHeader("Authorization", `Bearer ${token}`); }
				request.upload.onprogress = (event) => {
					if (event.lengthComputable) { setUploadProgress(Math.round((event.loaded / event.total) * 100)); }
				};
				request.onerror = () => reject(new Error("Upload connection failed"));
				request.onload = () => {
					let body: unknown;
					try { body = JSON.parse(request.responseText); } catch { body = null; }
					if (request.status < 200 || request.status >= 300) {
						const apiError = body as { error?: string } | null;
						reject(new Error(apiError?.error || `Upload failed with status ${request.status}`));
						return;
					}
					resolve(body as UploadVODResponse);
				};
				request.send(form);
			});
			setUploadProgress(100);
			await loadVods();
			setSelectedLabel(payload.vod.label);
			return true;
		} catch (err) {
			setError(messageFromError(err));
			return false;
		} finally {
			setUploadingVod(false);
		}
	}

	async function updateVOD(label: string, input: UpdateVODInput): Promise<boolean> {
		if (mutatingVod) { return false; }
		setMutatingVod(label);
		setError("");
		try {
			const response = await apiFetch(`/api/vods/${encodeURIComponent(label)}`, {
				method: "PATCH",
				headers: jsonHeaders,
				body: JSON.stringify(input)
			});
			if (!response.ok) { throw new Error(await readError(response)); }
			await loadVods();
			return true;
		} catch (err) {
			setError(messageFromError(err));
			return false;
		} finally {
			setMutatingVod("");
		}
	}

	async function deleteVOD(label: string): Promise<boolean> {
		if (mutatingVod) { return false; }
		setMutatingVod(label);
		setError("");
		try {
			const response = await apiFetch(`/api/vods/${encodeURIComponent(label)}`, {
				method: "DELETE",
				headers: authHeaders
			});
			if (!response.ok) { throw new Error(await readError(response)); }
			if (selectedLabel === label) { setSelectedLabel(""); }
			await loadVods();
			return true;
		} catch (err) {
			setError(messageFromError(err));
			return false;
		} finally {
			setMutatingVod("");
		}
	}

  function selectVOD(label: string) {
    setSelectedLabel(label);
    setPage("review");
  }

  function seekVideo(seconds: number) {
    const player = videoRef.current;
    if (!player) {
      return;
    }
    player.currentTime = Math.max(0, seconds);
    void player.play().catch(() => undefined);
  }

  if (loading && !user) {
    return <LoadingScreen />;
  }
  if (!user) {
    return (
      <AuthScreen
        authEmail={authEmail}
        authMode={authMode}
        authName={authName}
        authPassword={authPassword}
        error={error}
        setAuthEmail={setAuthEmail}
        setAuthMode={setAuthMode}
        setAuthName={setAuthName}
        setAuthPassword={setAuthPassword}
        submitAuth={submitAuth}
      />
    );
  }

  return (
    <main className="product-shell">
      <aside className="app-nav">
        <div className="brand-lockup product-brand">
          <div className="brand-mark">
            <Crosshair size={22} />
          </div>
          <div>
            <div className="brand-title">VOD COACH</div>
            <div className="brand-subtitle">TACTICAL REVIEW OS</div>
          </div>
        </div>

        <nav className="nav-stack" aria-label="Primary">
          <NavButton active={page === "dashboard"} icon={<Gauge size={18} />} label="Dashboard" onClick={() => setPage("dashboard")} />
          <NavButton active={page === "library"} icon={<Database size={18} />} label="Library" onClick={() => setPage("library")} />
          <NavButton active={page === "review"} icon={<Play size={18} />} label="Review" onClick={() => setPage("review")} />
          <NavButton active={page === "reports"} icon={<FileText size={18} />} label="Reports" onClick={() => setPage("reports")} />
          {user.role === "admin" && <NavButton active={page === "admin"} icon={<Shield size={18} />} label="Admin" onClick={() => setPage("admin")} />}
        </nav>

        <div className="nav-user">
          <span>{user.role}</span>
          <strong>{user.display_name}</strong>
          <small>{user.email}</small>
          <button onClick={() => void logout()} type="button">
            <LogOut size={15} />
            Sign out
          </button>
        </div>
      </aside>

      <section className="product-main">
        {error && (
          <div className="error-banner product-error">
            <AlertTriangle size={18} />
            {error}
          </div>
        )}

        {page === "dashboard" && (
          <DashboardPage
            backendHealth={backendHealth}
            counts={counts}
            latestReportSummary={latestReportSummary}
            report={report}
            selectedVod={selectedVod}
            setPage={setPage}
          />
        )}
        {page === "library" && (
		  <LibraryPage
			deleteVOD={deleteVOD}
			filteredVods={filteredVods}
            loading={loading}
            query={query}
            rank={rank}
            refresh={() => void loadVods()}
            selectedLabel={selectedLabel}
            selectVOD={selectVOD}
            setQuery={setQuery}
			setRank={setRank}
			mutatingVod={mutatingVod}
			updateVOD={updateVOD}
			uploading={uploadingVod}
			uploadProgress={uploadProgress}
			uploadVOD={uploadVOD}
          />
        )}
        {page === "review" && (
          <ReviewPage
            analysisJob={analysisJob}
            analyzing={analyzing}
			coachAssessments={coachAssessments}
			coachProgress={coachProgress}
            fullVod={fullVod}
            report={report}
			rateCoachRecommendation={(windowID, verdict) => void rateCoachRecommendation(windowID, verdict)}
            runAnalysis={() => void runAnalysis()}
            runDuration={runDuration}
            runFps={runFps}
			saveCoachAssessment={(windowID, answers) => void saveCoachAssessment(windowID, answers)}
			savingCoachReview={savingCoachReview}
            selectedVod={selectedVod}
            setFullVod={setFullVod}
            setRunDuration={setRunDuration}
            setRunFps={setRunFps}
            setPage={setPage}
            videoRef={videoRef}
            seekVideo={seekVideo}
          />
        )}
        {page === "reports" && (
          <ReportsPage
			coachAssessments={coachAssessments}
			coachAssessmentsPath={coachAssessmentsPath}
			coachProgress={coachProgress}
            correctionComment={correctionComment}
            correctionTargetID={correctionTargetID}
            correctionTargets={correctionTargets}
            correctionType={correctionType}
            correctionValue={correctionValue}
            evaluating={evaluating}
            evaluationAnnotations={evaluationAnnotations}
            evaluationHistory={evaluationHistory}
            latestReportSummary={latestReportSummary}
            loadReport={loadReport}
            manualCorrections={manualCorrections}
            manualCorrectionsPath={manualCorrectionsPath}
            report={report}
            reportHistory={reportHistory}
            runEvaluation={() => void runEvaluation()}
            saveManualCorrection={() => void saveManualCorrection()}
            savingCorrection={savingCorrection}
            selectedVod={selectedVod}
            setCorrectionComment={setCorrectionComment}
            setCorrectionTargetID={setCorrectionTargetID}
            setCorrectionType={setCorrectionType}
            setCorrectionValue={setCorrectionValue}
            seekVideo={seekVideo}
          />
        )}
        {page === "admin" && user.role === "admin" && (
          <AdminPage
            adminLogs={adminLogs}
            adminMetrics={adminMetrics}
            adminOverview={adminOverview}
            adminUsers={adminUsers}
            refresh={() => void loadAdmin()}
          />
        )}
      </section>
    </main>
  );
}

function AuthScreen(props: {
  authEmail: string;
  authMode: "login" | "register";
  authName: string;
  authPassword: string;
  error: string;
  setAuthEmail: (value: string) => void;
  setAuthMode: (value: "login" | "register") => void;
  setAuthName: (value: string) => void;
  setAuthPassword: (value: string) => void;
  submitAuth: () => void;
}) {
  return (
    <main className="auth-shell">
      <section className="auth-panel">
        <div className="brand-lockup">
          <div className="brand-mark">
            <Crosshair size={24} />
          </div>
          <div>
            <div className="brand-title">VOD COACH</div>
            <div className="brand-subtitle">LOCAL MVP</div>
          </div>
        </div>
        <div className="auth-mode">
          <button className={props.authMode === "login" ? "active" : ""} onClick={() => props.setAuthMode("login")} type="button">
            Sign in
          </button>
          <button className={props.authMode === "register" ? "active" : ""} onClick={() => props.setAuthMode("register")} type="button">
            Register
          </button>
        </div>
        {props.authMode === "register" && (
          <label>
            <span>Name</span>
            <input value={props.authName} onChange={(event) => props.setAuthName(event.target.value)} placeholder="Coach" />
          </label>
        )}
        <label>
          <span>Email</span>
          <input value={props.authEmail} onChange={(event) => props.setAuthEmail(event.target.value)} placeholder="coach@example.com" type="email" />
        </label>
        <label>
          <span>Password</span>
          <input value={props.authPassword} onChange={(event) => props.setAuthPassword(event.target.value)} placeholder="minimum 8 chars" type="password" />
        </label>
        {props.error && (
          <div className="auth-error">
            <AlertTriangle size={16} />
            {props.error}
          </div>
        )}
        <button className="auth-submit" onClick={props.submitAuth} type="button">
          <Shield size={17} />
          {props.authMode === "login" ? "Sign in" : "Create account"}
        </button>
      </section>
    </main>
  );
}

function DashboardPage(props: {
  backendHealth: BackendHealth | null;
  counts: VODListResponse["counts"] | null;
  latestReportSummary: ReportSummary | null;
  report: Report | null;
  selectedVod: VODItem | null;
  setPage: (page: PageID) => void;
}) {
  const coach = props.report?.gameplay?.coach;
  return (
    <>
      <PageHeader eyebrow="Overview" title="Your VOD review workspace" detail="Player workflow first, developer details in Admin." />
      <div className="stat-grid">
        <Metric icon={<Database size={18} />} label="Downloaded" value={props.counts ? `${props.counts.downloaded}/${props.counts.enabled}` : "..."} detail="local VODs" />
        <Metric icon={<FileText size={18} />} label="Reports" value={String(props.counts?.reported ?? 0)} detail="VODs reviewed" />
        <Metric icon={<Radar size={18} />} label="Model review" value={props.backendHealth?.model_review_available ? "online" : "offline"} detail={props.backendHealth?.vision_service?.model ?? "vision-service"} />
        <Metric icon={<Gauge size={18} />} label="Schema" value={String(props.backendHealth?.schema_version ?? 0)} detail={props.backendHealth?.analyzer ?? "unknown"} />
      </div>

      <div className="page-grid two">
        <section className="surface hero-surface">
          <div className="surface-heading">
            <div>
              <p className="eyebrow">Current analysis</p>
              <h2>{props.selectedVod?.title ?? "Select a VOD"}</h2>
            </div>
            <span className={props.backendHealth?.status === "ok" ? "success-chip" : "live-chip"}>
              <Activity size={14} />
              {props.backendHealth?.status ?? "unknown"}
            </span>
          </div>
          <div className="analysis-explainer">
            <StepPill index="1" title="Sample" detail="ffprobe and frames" />
            <StepPill index="2" title="Signals" detail="motion, HUD, minimap" />
            <StepPill index="3" title="Windows" detail="review clips" />
            <StepPill index="4" title="Report" detail="coach schema" />
          </div>
          <p className="plain-copy">
            The current engine is a deterministic visual baseline. It finds review moments from sampled frames and generates coach-ready evidence. Real VLM reasoning is optional through the vision service and is used only on selected clips.
          </p>
          <div className="hero-actions">
            <button onClick={() => props.setPage("library")} type="button">
              <Database size={16} />
              Open library
            </button>
            <button onClick={() => props.setPage("review")} type="button">
              <Play size={16} fill="currentColor" />
              Review VOD
            </button>
          </div>
        </section>

        <section className="surface">
          <div className="surface-heading">
            <div>
              <p className="eyebrow">Latest report</p>
              <h2>{props.latestReportSummary?.run_id ?? "No report loaded"}</h2>
            </div>
            <History size={19} />
          </div>
          {coach ? (
            <div className="coach-card">
              <strong>{coach.focus_areas?.[0]?.title ?? "Coach summary"}</strong>
              <p>{coach.verdict}</p>
              <span>{Math.round(clamp01(coach.confidence) * 100)}% confidence</span>
            </div>
          ) : (
            <EmptyState title="No gameplay summary" detail="Run analysis or select a report from the Reports page." />
          )}
        </section>
      </div>
    </>
  );
}

function LibraryPage(props: {
	deleteVOD: (label: string) => Promise<boolean>;
  filteredVods: VODItem[];
  loading: boolean;
	mutatingVod: string;
  query: string;
  rank: string;
  refresh: () => void;
  selectedLabel: string;
  selectVOD: (label: string) => void;
  setQuery: (value: string) => void;
  setRank: (value: string) => void;
	updateVOD: (label: string, input: UpdateVODInput) => Promise<boolean>;
	uploading: boolean;
	uploadProgress: number;
	uploadVOD: (input: UploadVODInput) => Promise<boolean>;
}) {
	const [showUpload, setShowUpload] = useState(false);
	const [uploadTitle, setUploadTitle] = useState("");
	const [uploadRank, setUploadRank] = useState("diamond");
	const [uploadMap, setUploadMap] = useState("");
	const [uploadAgent, setUploadAgent] = useState("");
	const [uploadFile, setUploadFile] = useState<File | null>(null);
	const [editingVod, setEditingVod] = useState<VODItem | null>(null);
	const [deletingVod, setDeletingVod] = useState<VODItem | null>(null);
	const [editTitle, setEditTitle] = useState("");
	const [editRank, setEditRank] = useState("diamond");
	const [editMap, setEditMap] = useState("");
	const [editAgent, setEditAgent] = useState("");

	async function submitUpload(event: FormEvent<HTMLFormElement>) {
		event.preventDefault();
		if (!uploadFile || !uploadTitle.trim()) { return; }
		const success = await props.uploadVOD({
			file: uploadFile, title: uploadTitle.trim(), rank: uploadRank,
			map: uploadMap.trim(), agent: uploadAgent.trim()
		});
		if (!success) { return; }
		setShowUpload(false);
		setUploadTitle("");
		setUploadMap("");
		setUploadAgent("");
		setUploadFile(null);
	}

	function openEdit(vod: VODItem) {
		setEditingVod(vod);
		setEditTitle(vod.title);
		setEditRank(vod.rank);
		setEditMap(vod.map ?? "");
		setEditAgent(vod.agent ?? "");
	}

	async function submitEdit(event: FormEvent<HTMLFormElement>) {
		event.preventDefault();
		if (!editingVod || !editTitle.trim()) { return; }
		const success = await props.updateVOD(editingVod.label, {
			title: editTitle.trim(), rank: editRank, map: editMap.trim(), agent: editAgent.trim()
		});
		if (success) { setEditingVod(null); }
	}

	async function confirmDelete() {
		if (!deletingVod) { return; }
		const success = await props.deleteVOD(deletingVod.label);
		if (success) { setDeletingVod(null); }
	}

  return (
    <>
	  <PageHeader eyebrow="Library" title="VOD library" detail="Pick a match and move into review." action={<div className="page-actions"><button className="command-button" onClick={() => setShowUpload(true)} type="button"><Upload size={16} />Upload VOD</button><IconButton icon={<RefreshCw size={17} />} onClick={props.refresh} title="Refresh" /></div>} />
      <section className="surface">
        <div className="library-toolbar">
          <div className="search-box product-search">
            <Search size={16} />
            <input value={props.query} onChange={(event) => props.setQuery(event.target.value)} placeholder="Search VOD, rank, channel" />
          </div>
          <div className="rank-strip">
            {ranks.map((rankOption) => (
              <button className={props.rank === rankOption ? "rank-pill active" : "rank-pill"} key={rankOption} onClick={() => props.setRank(rankOption)} type="button">
                {rankOption}
              </button>
            ))}
          </div>
        </div>
        <div className="vod-table">
          {props.loading ? (
            <EmptyState title="Loading VODs" detail="Dataset scan is running." />
          ) : (
            props.filteredVods.map((vod) => (
              <article className={props.selectedLabel === vod.label ? "vod-card active" : "vod-card"} key={vod.label}>
				<button className="vod-card-open" onClick={() => props.selectVOD(vod.label)} type="button">
				  <span className={`rank-sigil rank-${vod.rank}`}>{vod.rank.slice(0, 3)}</span>
				  <div>
					<strong>{vod.title}</strong>
					<small>{vod.source_type === "upload" ? [vod.agent, vod.map, vod.rank, vod.duration_text].filter(Boolean).join(" / ") : `${vod.channel} / ${vod.duration_text} / ${vod.label}`}</small>
				  </div>
				  <em className={vod.local_status === "downloaded" ? "ready" : ""}>{vod.local_status}</em>
				  <span>{vod.report_count} reports</span>
				</button>
				{vod.source_type === "upload" && (
				  <div className="vod-card-actions">
					<button disabled={Boolean(props.mutatingVod)} onClick={() => openEdit(vod)} title="Edit VOD" type="button"><Pencil size={16} /></button>
					<button className="danger" disabled={Boolean(props.mutatingVod)} onClick={() => setDeletingVod(vod)} title="Delete VOD" type="button"><Trash2 size={16} /></button>
				  </div>
				)}
              </article>
            ))
          )}
        </div>
      </section>

	  {showUpload && (
		<div className="modal-backdrop" onMouseDown={(event) => { if (event.target === event.currentTarget && !props.uploading) { setShowUpload(false); } }}>
		  <section aria-labelledby="upload-title" aria-modal="true" className="upload-dialog" role="dialog">
			<header><div><p className="eyebrow">New review</p><h2 id="upload-title">Upload VOD</h2></div><button className="icon-button" disabled={props.uploading} onClick={() => setShowUpload(false)} title="Close upload" type="button"><X size={18} /></button></header>
			<form onSubmit={submitUpload}>
			  <label className="upload-file-field">
				<FileVideo2 size={25} />
				<span>{uploadFile?.name ?? "Select video file"}</span>
				<small>{uploadFile ? `${(uploadFile.size / 1024 / 1024).toFixed(1)} MB` : "MP4, MKV, WebM, MOV"}</small>
				<input accept="video/mp4,video/x-matroska,video/webm,video/quicktime,.mp4,.mkv,.webm,.mov" disabled={props.uploading} onChange={(event) => setUploadFile(event.target.files?.[0] ?? null)} type="file" />
			  </label>
			  <label><span>Title</span><input disabled={props.uploading} maxLength={160} onChange={(event) => setUploadTitle(event.target.value)} placeholder="Ranked game — Bind" required value={uploadTitle} /></label>
			  <div className="upload-metadata-grid">
				<label><span>Rank</span><select disabled={props.uploading} onChange={(event) => setUploadRank(event.target.value)} value={uploadRank}>{ranks.filter((item) => item !== "all").map((item) => <option key={item} value={item}>{item}</option>)}</select></label>
				<label><span>Map</span><input disabled={props.uploading} maxLength={64} onChange={(event) => setUploadMap(event.target.value)} placeholder="Bind" value={uploadMap} /></label>
				<label><span>Agent</span><input disabled={props.uploading} maxLength={64} onChange={(event) => setUploadAgent(event.target.value)} placeholder="Sova" value={uploadAgent} /></label>
			  </div>
			  {props.uploading && <div className="upload-progress"><div><i style={{ width: `${props.uploadProgress}%` }} /></div><span>{props.uploadProgress < 100 ? `${props.uploadProgress}% uploading` : "Validating video"}</span></div>}
			  <footer><button className="secondary-action" disabled={props.uploading} onClick={() => setShowUpload(false)} type="button">Cancel</button><button className="run-button" disabled={props.uploading || !uploadFile || !uploadTitle.trim()} type="submit"><Upload size={17} />{props.uploading ? "Uploading" : "Upload"}</button></footer>
			</form>
		  </section>
		</div>
	  )}

	  {editingVod && (
		<div className="modal-backdrop" onMouseDown={(event) => { if (event.target === event.currentTarget && !props.mutatingVod) { setEditingVod(null); } }}>
		  <section aria-labelledby="edit-vod-title" aria-modal="true" className="upload-dialog manage-vod-dialog" role="dialog">
			<header><div><p className="eyebrow">Library</p><h2 id="edit-vod-title">Edit VOD</h2></div><button className="icon-button" disabled={Boolean(props.mutatingVod)} onClick={() => setEditingVod(null)} title="Close" type="button"><X size={18} /></button></header>
			<form onSubmit={submitEdit}>
			  <label><span>Title</span><input disabled={Boolean(props.mutatingVod)} maxLength={160} onChange={(event) => setEditTitle(event.target.value)} required value={editTitle} /></label>
			  <div className="upload-metadata-grid">
				<label><span>Rank</span><select disabled={Boolean(props.mutatingVod)} onChange={(event) => setEditRank(event.target.value)} value={editRank}>{ranks.filter((item) => item !== "all").map((item) => <option key={item} value={item}>{item}</option>)}</select></label>
				<label><span>Map</span><input disabled={Boolean(props.mutatingVod)} maxLength={64} onChange={(event) => setEditMap(event.target.value)} value={editMap} /></label>
				<label><span>Agent</span><input disabled={Boolean(props.mutatingVod)} maxLength={64} onChange={(event) => setEditAgent(event.target.value)} value={editAgent} /></label>
			  </div>
			  <footer><button className="secondary-action" disabled={Boolean(props.mutatingVod)} onClick={() => setEditingVod(null)} type="button">Cancel</button><button className="run-button" disabled={Boolean(props.mutatingVod) || !editTitle.trim()} type="submit"><CheckCircle2 size={17} />{props.mutatingVod ? "Saving" : "Save"}</button></footer>
			</form>
		  </section>
		</div>
	  )}

	  {deletingVod && (
		<div className="modal-backdrop" onMouseDown={(event) => { if (event.target === event.currentTarget && !props.mutatingVod) { setDeletingVod(null); } }}>
		  <section aria-labelledby="delete-vod-title" aria-modal="true" className="confirm-dialog" role="alertdialog">
			<div className="confirm-dialog-icon"><Trash2 size={22} /></div>
			<h2 id="delete-vod-title">Delete VOD?</h2>
			<p><strong>{deletingVod.title}</strong> and its generated review artifacts will be permanently removed.</p>
			<footer><button className="secondary-action" disabled={Boolean(props.mutatingVod)} onClick={() => setDeletingVod(null)} type="button">Cancel</button><button className="danger-command" disabled={Boolean(props.mutatingVod)} onClick={() => void confirmDelete()} type="button"><Trash2 size={16} />{props.mutatingVod ? "Deleting" : "Delete"}</button></footer>
		  </section>
		</div>
	  )}
    </>
  );
}

function ReviewPage(props: {
  analysisJob: AnalysisJobResponse | null;
  analyzing: boolean;
	coachAssessments: GuidedReviewAssessment[];
	coachProgress: CoachReviewProgress | null;
  fullVod: boolean;
	rateCoachRecommendation: (windowID: string, verdict: string) => void;
  report: Report | null;
  runAnalysis: () => void;
  runDuration: number;
  runFps: string;
	saveCoachAssessment: (windowID: string, answers: Record<string, string>) => void;
	savingCoachReview: boolean;
  selectedVod: VODItem | null;
  setFullVod: (value: boolean) => void;
  setRunDuration: (value: number) => void;
  setRunFps: (value: string) => void;
  setPage: (page: PageID) => void;
  videoRef: RefObject<HTMLVideoElement | null>;
  seekVideo: (seconds: number) => void;
}) {
	const review = props.report?.gameplay?.coach_review;
	const decisions = review?.decisions ?? [];
	const [selectedDecisionID, setSelectedDecisionID] = useState("");
	const [answers, setAnswers] = useState<Record<string, string>>({});
	const selectedDecision = decisions.find((decision) => decision.id === selectedDecisionID) ?? decisions[0] ?? null;
	const assessment = props.coachAssessments.find((item) => item.window_id === selectedDecision?.window_id) ?? null;
	const result = assessment?.result ?? null;

	useEffect(() => {
		if (decisions.length === 0) { setSelectedDecisionID(""); return; }
		if (!decisions.some((decision) => decision.id === selectedDecisionID)) {
			const pending = decisions.find((decision) => !props.coachAssessments.some((item) => item.window_id === decision.window_id));
			setSelectedDecisionID((pending ?? decisions[0]).id);
		}
	}, [props.report?.run_id, decisions.length]);

	useEffect(() => {
		setAnswers(assessment?.answers ?? {});
	}, [selectedDecision?.window_id, assessment?.updated_at]);

	function chooseDecision(decision: CoachDecision) {
		setSelectedDecisionID(decision.id);
		props.seekVideo(decision.timestamp_seconds);
	}

  return (
    <>
      <PageHeader eyebrow="Review" title={props.selectedVod?.title ?? "Select a VOD"} detail={props.selectedVod ? `${props.selectedVod.rank} / ${props.selectedVod.duration_text}` : "Choose a VOD from Library."} />
      <div className="review-layout">
        <section className="surface">
          <div className="video-stage product-video">
            {props.selectedVod?.video_url ? (
              <video controls preload="metadata" ref={props.videoRef} src={apiURL(props.selectedVod.video_url)} />
            ) : (
              <div className="video-placeholder">
                <Video size={30} />
                <span>{props.selectedVod ? "Not downloaded" : "No VOD selected"}</span>
              </div>
            )}
          </div>
          <div className="run-controls product-controls">
            <label>
              <span>Sample seconds</span>
              <input disabled={props.fullVod} min={30} max={600} step={30} type="number" value={props.runDuration} onChange={(event) => props.setRunDuration(Number(event.target.value))} />
            </label>
            <label>
              <span>FPS</span>
              <select value={props.runFps} onChange={(event) => props.setRunFps(event.target.value)}>
                <option value="0.5">0.5</option>
                <option value="1">1</option>
                <option value="2">2</option>
              </select>
            </label>
            <label className="toggle-control">
              <input checked={props.fullVod} onChange={(event) => props.setFullVod(event.target.checked)} type="checkbox" />
              <span>Full VOD</span>
            </label>
            <button className="run-button" disabled={!props.selectedVod || props.selectedVod.local_status !== "downloaded" || props.analyzing} onClick={props.runAnalysis} type="button">
              <Play size={18} fill="currentColor" />
              {props.analyzing ? "Analyzing" : props.fullVod ? "Run full VOD" : "Run analysis"}
            </button>
          </div>
          {props.analysisJob && (
            <div className={`analysis-job status-${props.analysisJob.status}`}>
              <span>{props.analysisJob.status}</span>
              <strong>{props.analysisJob.run_id}</strong>
              <small>{props.analysisJob.message ?? props.analysisJob.job_id}</small>
            </div>
          )}
        </section>

        <section className="surface">
          <div className="surface-heading">
            <div>
				<p className="eyebrow">Guided review queue</p>
				<h2>{props.coachProgress ? `${props.coachProgress.completed} / ${props.coachProgress.total} complete` : props.report?.run_id ?? "No report"}</h2>
            </div>
			{review && <span className={`quality-chip quality-${review.evidence_quality.level}`}><Eye size={14} />{Math.round(review.evidence_quality.score * 100)}% evidence</span>}
          </div>
		  {review ? (
			<>
			  <div className="review-progress" aria-label="Guided review progress"><i style={{ width: `${review.decisions.length ? ((props.coachProgress?.completed ?? 0) / review.decisions.length) * 100 : 0}%` }} /></div>
			  <p className="review-contract-copy">{review.summary}</p>
			  <div className="coach-queue">
				{decisions.map((decision) => {
				  const saved = props.coachAssessments.find((item) => item.window_id === decision.window_id);
				  return (
					<button className={selectedDecision?.id === decision.id ? "coach-queue-row active" : "coach-queue-row"} key={decision.id} onClick={() => chooseDecision(decision)} type="button">
					  <span className={`queue-status assessment-${saved?.result.assessment ?? "pending"}`}>{saved ? <CheckCircle2 size={15} /> : <Clock3 size={15} />}</span>
					  <span><strong>{decision.title}</strong><small>{decision.kind.replaceAll("_", " ")} / {formatSeconds(decision.timestamp_seconds)}{decision.round_number ? ` / R${decision.round_number}` : ""}</small></span>
					  <ChevronRight size={16} />
					</button>
				  );
				})}
			  </div>
			  <button className="secondary-action" onClick={() => props.setPage("reports")} type="button"><FileText size={16} />Open validated report</button>
			</>
          ) : (
			<EmptyState title="No evidence-first report" detail={props.report ? "This report has no gameplay windows. Run a full VOD analysis." : "Run analysis or pick a report from history."} />
          )}
        </section>
      </div>

	  {selectedDecision && (
		<section className="surface guided-review-workspace">
		  <div className="guided-review-header">
			<div>
			  <p className="eyebrow">Evidence check / {selectedDecision.rule_id}</p>
			  <h2>{result?.title ?? selectedDecision.title}</h2>
			  <p>{result?.observation ?? selectedDecision.observation}</p>
			</div>
			<button className="seek-button" onClick={() => props.seekVideo(selectedDecision.timestamp_seconds)} type="button"><Play size={14} fill="currentColor" />{formatSeconds(selectedDecision.timestamp_seconds)}</button>
		  </div>

		  <div className="guided-review-columns">
			<form className="coach-questionnaire" onSubmit={(event) => { event.preventDefault(); props.saveCoachAssessment(selectedDecision.window_id, answers); }}>
			  <div className="questionnaire-heading"><span>Confirm only visible facts</span><strong>{Object.keys(answers).length} answered</strong></div>
			  {(selectedDecision.questions ?? []).map((question) => (
				<fieldset key={question.id}>
				  <legend>{question.prompt}</legend>
				  <div className="answer-options">
					{question.options.map((option) => (
					  <button className={answers[question.id] === option.value ? "active" : ""} key={option.value} onClick={() => setAnswers((current) => ({ ...current, [question.id]: option.value }))} type="button">{option.label}</button>
					))}
				  </div>
				</fieldset>
			  ))}
			  <button className="run-button coach-submit" disabled={props.savingCoachReview || Object.keys(answers).length === 0} type="submit"><CheckCircle2 size={17} />{props.savingCoachReview ? "Evaluating" : assessment ? "Update assessment" : "Generate assessment"}</button>
			</form>

			<div className={`coach-assessment-output assessment-${result?.assessment ?? "pending"}`}>
			  {result ? (
				<>
				  <div className="assessment-title"><span>{result.assessment.replaceAll("_", " ")} / {Math.round(result.confidence * 100)}%</span><h3>{result.title}</h3><p>{result.observation}</p></div>
				  {result.recommendation ? (
					<div className="recommendation-body">
					  <section><span>Correction</span><strong>{result.recommendation.summary}</strong><p>{result.recommendation.why_it_matters}</p></section>
					  <section><span>Better action</span><p>{result.recommendation.better_action}</p></section>
					  <section><span>Drill</span><p>{result.recommendation.drill}</p></section>
					  <blockquote>{result.recommendation.checkpoint}</blockquote>
					  <div className="coach-feedback"><span>Was this useful?</span><button className={assessment?.feedback?.verdict === "useful" ? "active" : ""} title="Useful" onClick={() => props.rateCoachRecommendation(selectedDecision.window_id, "useful")} type="button"><ThumbsUp size={16} /></button><button className={assessment?.feedback?.verdict === "not_useful" ? "active" : ""} title="Not useful" onClick={() => props.rateCoachRecommendation(selectedDecision.window_id, "not_useful")} type="button"><ThumbsDown size={16} /></button><button className={assessment?.feedback?.verdict === "incorrect" ? "active danger" : "danger"} title="Incorrect" onClick={() => props.rateCoachRecommendation(selectedDecision.window_id, "incorrect")} type="button"><XCircle size={16} /></button></div>
					</div>
				  ) : <div className="neutral-result"><CheckCircle2 size={20} /><p>{result.why_review}</p></div>}
				</>
			  ) : (
				<div className="assessment-placeholder"><Eye size={28} /><h3>No conclusion yet</h3><p>{selectedDecision.why_review}</p></div>
			  )}
			</div>
		  </div>
		</section>
	  )}
    </>
  );
}

function ReportsPage(props: {
	coachAssessments: GuidedReviewAssessment[];
	coachAssessmentsPath: string;
	coachProgress: CoachReviewProgress | null;
  correctionComment: string;
  correctionTargetID: string;
  correctionTargets: Array<{ id: string; label: string }>;
  correctionType: string;
  correctionValue: string;
  evaluating: boolean;
  evaluationAnnotations: EvaluationAnnotationSummary[];
  evaluationHistory: EvaluationSummary[];
  latestReportSummary: ReportSummary | null;
  loadReport: (label: string, runID: string) => Promise<void>;
  manualCorrections: ManualCorrection[];
  manualCorrectionsPath: string;
  report: Report | null;
  reportHistory: ReportSummary[];
  runEvaluation: () => void;
  saveManualCorrection: () => void;
  savingCorrection: boolean;
  selectedVod: VODItem | null;
  setCorrectionComment: (value: string) => void;
  setCorrectionTargetID: (value: string) => void;
  setCorrectionType: (value: string) => void;
  setCorrectionValue: (value: string) => void;
  seekVideo: (seconds: number) => void;
}) {
  const windows = props.report?.gameplay?.review_windows ?? [];
	const actionable = props.coachAssessments.filter((item) => item.result.recommendation);
  return (
    <>
      <PageHeader eyebrow="Reports" title="Review evidence and corrections" detail={props.report ? `Run ${props.report.run_id}` : "No report selected."} />
      <div className="page-grid reports-grid">
        <section className="surface">
          <div className="surface-heading">
            <div>
              <p className="eyebrow">History</p>
              <h2>{props.selectedVod?.label ?? "No VOD"}</h2>
            </div>
            <History size={19} />
          </div>
          <div className="history-list product-history">
            {props.reportHistory.map((item) => (
              <button className={props.report?.run_id === item.run_id ? "history-run active" : "history-run"} key={item.run_id} onClick={() => props.selectedVod && void props.loadReport(props.selectedVod.label, item.run_id)} type="button">
                <span>{item.run_id}</span>
                <small>{item.frame_count} frames / {item.review_window_count} windows / {item.model_review_run_count || 0}/{item.model_review_task_count || 0} model</small>
                <small>{item.analyzer ?? `schema ${item.schema_version}`}</small>
              </button>
            ))}
            {props.reportHistory.length === 0 && <EmptyState title="No reports" detail="Run analysis first." />}
          </div>
          {props.latestReportSummary && (
            <div className="artifact-actions">
              <a href={artifactURL(props.latestReportSummary.json_path)} target="_blank" rel="noreferrer">
                <FileJson2 size={15} />
                JSON
              </a>
              <a href={artifactURL(props.latestReportSummary.markdown_path)} target="_blank" rel="noreferrer">
                <FileText size={15} />
                Markdown
              </a>
            </div>
          )}
        </section>

        <section className="surface">
          <div className="surface-heading">
            <div>
              <p className="eyebrow">Review windows</p>
              <h2>{windows.length} moments</h2>
            </div>
            <Radar size={19} />
          </div>
          <div className="window-list compact-window-list">
            {windows.slice(0, 8).map((window) => (
              <article className={`review-window severity-${window.severity}`} key={window.id}>
                <div className="review-window-head">
                  <div>
                    <span>{window.kind.replaceAll("_", " ")} / {windowRange(window)}</span>
                    <h3>{window.title}</h3>
                  </div>
                  <button className="seek-button" onClick={() => props.seekVideo(window.peak_seconds)} type="button">
                    <Play size={13} fill="currentColor" />
                    {formatSeconds(window.peak_seconds)}
                  </button>
                </div>
                <p>{window.summary}</p>
              </article>
            ))}
            {windows.length === 0 && <EmptyState title="No windows" detail="This report has no gameplay windows." />}
          </div>
        </section>

        <section className="surface validated-coaching">
          <div className="surface-heading">
            <div>
			  <p className="eyebrow">Validated coaching</p>
			  <h2>{props.coachProgress?.actionable ?? 0} actionable / {props.coachProgress?.completed ?? 0} reviewed</h2>
            </div>
            <Lightbulb size={19} />
          </div>
		  <div className="validated-list">
			{actionable.map((item) => (
			  <article className={`validated-row severity-${item.result.severity}`} key={item.id}>
				<div className="validated-row-head"><div><span>{item.result.rule_id} / {Math.round(item.result.confidence * 100)}%</span><h3>{item.result.title}</h3></div><button className="seek-button" onClick={() => props.seekVideo(item.result.timestamp_seconds)} type="button"><Play size={13} fill="currentColor" />{formatSeconds(item.result.timestamp_seconds)}</button></div>
				<strong>{item.result.recommendation?.summary}</strong>
				<p>{item.result.recommendation?.better_action}</p>
				<small>Drill: {item.result.recommendation?.drill}</small>
			  </article>
			))}
			{actionable.length === 0 && <EmptyState title="No validated findings" detail="Complete guided reviews on the Review page. Automatic candidates are not presented as mistakes." />}
		  </div>
		  {props.coachAssessmentsPath && <div className="artifact-actions compact"><a href={artifactURL(props.coachAssessmentsPath)} target="_blank" rel="noreferrer"><FileJson2 size={13} />Coaching JSON</a></div>}
        </section>

        <section className="surface">
          <div className="surface-heading">
            <div>
              <p className="eyebrow">Corrections</p>
              <h2>{props.manualCorrections.length} saved</h2>
            </div>
            <CheckCircle2 size={19} />
          </div>
          <div className="correction-form product-correction-form">
            <label>
              <span>Type</span>
              <select value={props.correctionType} onChange={(event) => props.setCorrectionType(event.target.value)}>
                {correctionTypes.map((type) => <option key={type} value={type}>{type.replaceAll("_", " ")}</option>)}
              </select>
            </label>
            <label>
              <span>Target</span>
              <select value={props.correctionTargetID} onChange={(event) => props.setCorrectionTargetID(event.target.value)}>
                <option value="">report / general</option>
                {props.correctionTargets.map((target) => <option key={target.id} value={target.id}>{target.label}</option>)}
              </select>
            </label>
            <label>
              <span>Value</span>
              <input value={props.correctionValue} onChange={(event) => props.setCorrectionValue(event.target.value)} placeholder="Correct value" />
            </label>
            <label className="correction-comment">
              <span>Comment</span>
              <textarea value={props.correctionComment} onChange={(event) => props.setCorrectionComment(event.target.value)} placeholder="Why this should change" rows={3} />
            </label>
            <button disabled={props.savingCorrection || (!props.correctionValue.trim() && !props.correctionComment.trim())} onClick={props.saveManualCorrection} type="button">
              <CheckCircle2 size={15} />
              {props.savingCorrection ? "Saving" : "Add correction"}
            </button>
          </div>
          <div className="correction-list">
            {props.manualCorrections.slice(-4).reverse().map((correction) => (
              <article className="correction-card" key={correction.id}>
                <div>
                  <span>{correction.type.replaceAll("_", " ")}</span>
                  <strong>{correction.target_id || "report"}</strong>
                </div>
                {correction.corrected_value ? <p>{correction.corrected_value}</p> : null}
                {correction.comment ? <small>{correction.comment}</small> : null}
              </article>
            ))}
          </div>
          {props.manualCorrectionsPath && (
            <div className="artifact-actions compact">
              <a href={artifactURL(props.manualCorrectionsPath)} target="_blank" rel="noreferrer">
                <FileJson2 size={13} />
                Corrections JSON
              </a>
            </div>
          )}
        </section>

        <section className="surface wide">
          <div className="surface-heading">
            <div>
              <p className="eyebrow">Benchmarks</p>
              <h2>{props.evaluationHistory.length} eval runs</h2>
            </div>
            <button className="secondary-action inline" disabled={!props.report || props.evaluating || props.evaluationAnnotations.length === 0} onClick={props.runEvaluation} type="button">
              <BarChart3 size={15} />
              {props.evaluating ? "Running" : "Run benchmark"}
            </button>
          </div>
          <div className="quality-list product-quality">
            {props.evaluationHistory.slice(0, 4).map((item) => (
              <article className="quality-card" key={item.run_id}>
                <div>
                  <span>{item.run_id}</span>
                  <strong>{Math.round(clamp01(item.f1) * 100)}% F1</strong>
                </div>
                <p>{item.match_count}/{item.label_count} labels / {item.prediction_count} predictions / report {item.report_run_id}</p>
              </article>
            ))}
            {props.evaluationHistory.length === 0 && <EmptyState title="No benchmarks" detail="Add labels in ml/evals and run benchmark." />}
          </div>
        </section>
      </div>
    </>
  );
}

function AdminPage(props: {
  adminLogs: RequestLog[];
  adminMetrics: AdminMetricsResponse | null;
  adminOverview: AdminOverview | null;
  adminUsers: AuthUser[];
  refresh: () => void;
}) {
  const requests = props.adminMetrics?.requests ?? [];
  const readinessEntries = Object.entries(props.adminOverview?.readiness ?? {});
  const maxCount = Math.max(1, ...requests.map((item) => item.count));
  return (
    <>
      <PageHeader eyebrow="Admin" title="Operations console" detail="Local service diagnostics, request metrics, logs, users." action={<IconButton icon={<RefreshCw size={17} />} onClick={props.refresh} title="Refresh admin" />} />
      <div className="stat-grid">
        <Metric icon={<Database size={18} />} label="Dataset" value={String(props.adminOverview?.dataset.total ?? 0)} detail="manifest rows" />
        <Metric icon={<FileText size={18} />} label="Reports" value={String(props.adminOverview?.dataset.reported ?? 0)} detail="VODs ready" />
        <Metric icon={<Shield size={18} />} label="Users" value={String(props.adminOverview?.auth.user_count ?? 0)} detail="local auth" />
        <Metric icon={<Activity size={18} />} label="Jobs" value={String(props.adminOverview?.jobs.running ?? 0)} detail="running" />
      </div>
      <div className="page-grid two">
        <section className="surface">
          <div className="surface-heading">
            <div>
              <p className="eyebrow">HTTP metrics</p>
              <h2>Requests by route</h2>
            </div>
            <BarChart3 size={19} />
          </div>
          <div className="metric-bars">
            {requests.slice(0, 10).map((item) => (
              <div className="metric-bar" key={`${item.method}-${item.route}-${item.status}`}>
                <span>{item.method} {item.route}</span>
                <div><i style={{ width: `${Math.max(6, (item.count / maxCount) * 100)}%` }} /></div>
                <strong>{item.count}</strong>
              </div>
            ))}
            {requests.length === 0 && <EmptyState title="No request metrics" detail="Use the app and refresh admin." />}
          </div>
          <div className="admin-links">
            <a href={apiURL("/metrics")} target="_blank" rel="noreferrer">Prometheus</a>
            <a href={apiURL("/debug/pprof/")} target="_blank" rel="noreferrer">pprof</a>
            <a href={apiURL("/readyz")} target="_blank" rel="noreferrer">readyz</a>
          </div>
        </section>

        <section className="surface">
          <div className="surface-heading">
            <div>
              <p className="eyebrow">Readiness</p>
              <h2>Service checks</h2>
            </div>
            <CheckCircle2 size={19} />
          </div>
          <div className="log-list">
            {readinessEntries.map(([name, check]) => (
              <article className="log-row" key={name}>
                <span className={check.status === "ok" ? "ok" : check.status === "failed" ? "bad" : "warn"}>{check.status}</span>
                <div>
                  <strong>{name.replaceAll("_", " ")}</strong>
                  <small>{check.detail ?? check.path ?? check.runtime ?? check.model ?? "ready"}</small>
                </div>
              </article>
            ))}
            {readinessEntries.length === 0 && <EmptyState title="No readiness data" detail="Refresh admin diagnostics." />}
          </div>
        </section>

        <section className="surface">
          <div className="surface-heading">
            <div>
              <p className="eyebrow">Logs</p>
              <h2>Recent requests</h2>
            </div>
            <Activity size={19} />
          </div>
          <div className="log-list">
            {props.adminLogs.slice(0, 14).map((log) => (
              <article className="log-row" key={`${log.time}-${log.path}-${log.duration_ms}`}>
                <span className={log.status >= 500 ? "bad" : log.status >= 400 ? "warn" : "ok"}>{log.status}</span>
                <div>
                  <strong>{log.method} {log.route}</strong>
                  <small>{log.path} / {log.duration_ms.toFixed(1)}ms / {new Date(log.time).toLocaleTimeString()} / {log.user_email ?? "anonymous"}</small>
                </div>
              </article>
            ))}
            {props.adminLogs.length === 0 && <EmptyState title="No logs" detail="No requests recorded yet." />}
          </div>
        </section>

        <section className="surface">
          <div className="surface-heading">
            <div>
              <p className="eyebrow">Users</p>
              <h2>Local accounts</h2>
            </div>
            <Shield size={19} />
          </div>
          <div className="user-list">
            {props.adminUsers.map((account) => (
              <article className="user-row" key={account.id}>
                <span>{account.role}</span>
                <div>
                  <strong>{account.display_name}</strong>
                  <small>{account.email}</small>
                </div>
              </article>
            ))}
          </div>
        </section>

        <section className="surface">
          <div className="surface-heading">
            <div>
              <p className="eyebrow">System</p>
              <h2>Paths</h2>
            </div>
            <Gauge size={19} />
          </div>
          <dl className="system-list">
            <dt>manifest</dt>
            <dd>{props.adminOverview?.system.manifest_path}</dd>
            <dt>raw</dt>
            <dd>{props.adminOverview?.system.raw_root}</dd>
            <dt>processed</dt>
            <dd>{props.adminOverview?.system.processed_root}</dd>
            <dt>analyzer</dt>
            <dd>{props.adminOverview?.system.analyzer}</dd>
          </dl>
        </section>
      </div>
    </>
  );
}

function PageHeader(props: { eyebrow: string; title: string; detail: string; action?: ReactNode }) {
  return (
    <header className="page-header">
      <div>
        <p className="eyebrow">{props.eyebrow}</p>
        <h1>{props.title}</h1>
        <span>{props.detail}</span>
      </div>
      {props.action}
    </header>
  );
}

function NavButton(props: { active: boolean; icon: ReactNode; label: string; onClick: () => void }) {
  return (
    <button className={props.active ? "nav-button active" : "nav-button"} onClick={props.onClick} type="button">
      {props.icon}
      {props.label}
    </button>
  );
}

function IconButton(props: { icon: ReactNode; onClick: () => void; title: string }) {
  return (
    <button className="icon-button" onClick={props.onClick} title={props.title} type="button">
      {props.icon}
    </button>
  );
}

function Metric(props: { icon: ReactNode; label: string; value: string; detail: string; compact?: boolean }) {
  return (
    <div className={props.compact ? "metric compact" : "metric"}>
      <span className="metric-icon">{props.icon}</span>
      <div>
        <span>{props.label}</span>
        <strong>{props.value}</strong>
        <small>{props.detail}</small>
      </div>
    </div>
  );
}

function StepPill(props: { index: string; title: string; detail: string }) {
  return (
    <div className="step-pill">
      <span>{props.index}</span>
      <strong>{props.title}</strong>
      <small>{props.detail}</small>
    </div>
  );
}

function EmptyState(props: { title: string; detail: string }) {
  return (
    <div className="empty-state compact-empty">
      <Radar size={28} />
      <h3>{props.title}</h3>
      <p>{props.detail}</p>
    </div>
  );
}

function LoadingScreen() {
  return (
    <main className="auth-shell">
      <div className="loading-mark">
        <Crosshair size={28} />
      </div>
    </main>
  );
}

async function readError(response: Response) {
  try {
    const payload = (await response.json()) as { error?: string };
    return payload.error || response.statusText;
  } catch {
    return response.statusText;
  }
}

function messageFromError(error: unknown) {
  if (error instanceof Error) {
    return error.message;
  }
  return "Unknown error";
}

function sleep(ms: number) {
  return new Promise((resolve) => window.setTimeout(resolve, ms));
}

function compactTimestamp(date: Date) {
  const pad = (value: number) => String(value).padStart(2, "0");
  return `${date.getUTCFullYear()}${pad(date.getUTCMonth() + 1)}${pad(date.getUTCDate())}T${pad(date.getUTCHours())}${pad(date.getUTCMinutes())}${pad(date.getUTCSeconds())}Z`;
}

function formatSeconds(seconds: number) {
  return `${seconds.toFixed(seconds % 1 === 0 ? 0 : 1)}s`;
}

function windowRange(window: ReviewWindow) {
  return `${formatSeconds(window.start_seconds)}-${formatSeconds(window.end_seconds)}`;
}

function buildCorrectionTargets(report: Report | null) {
  if (!report) {
    return [];
  }
  const targets: Array<{ id: string; label: string }> = [{ id: `report:${report.run_id}`, label: `report / ${report.run_id}` }];
  for (const finding of report.findings ?? []) {
    targets.push({ id: finding.id, label: compactLabel(`finding / ${finding.title}`) });
  }
  for (const event of report.gameplay?.gameplay_events ?? []) {
    targets.push({ id: event.id, label: compactLabel(`event / ${formatSeconds(event.timestamp_seconds)} / ${event.title}`) });
  }
  for (const window of report.gameplay?.review_windows ?? []) {
    targets.push({ id: window.id, label: compactLabel(`window / ${windowRange(window)} / ${window.title}`) });
  }
  for (const round of report.gameplay?.round_segments ?? []) {
    targets.push({ id: `round:${round.round_number}`, label: compactLabel(`round ${round.round_number} / ${formatSeconds(round.start_seconds)}-${formatSeconds(round.end_seconds)}`) });
  }
  return targets;
}

function compactLabel(value: string) {
  return value.length > 88 ? `${value.slice(0, 85)}...` : value;
}

function clamp01(value: number) {
  if (!Number.isFinite(value)) {
    return 0;
  }
  return Math.min(1, Math.max(0, value));
}

function artifactURL(path: string) {
  if (!path) {
    return "#";
  }
  const normalized = path.replaceAll("\\", "/");
  const marker = "data/processed/";
  const index = normalized.indexOf(marker);
  if (index >= 0) {
    return apiURL(`/artifacts/${normalized.slice(index + marker.length)}`);
  }
  return apiURL(`/artifacts/${normalized.replace(/^\/+/, "")}`);
}

function apiURL(path: string) {
  const explicitBase = import.meta.env.VITE_API_BASE as string | undefined;
  const base = explicitBase || devBackendBase();
  return `${base}${path}`;
}

function apiFetch(path: string, init: RequestInit = {}) {
  return fetch(apiURL(path), { ...init, credentials: "include" });
}

function devBackendBase() {
  const isLocalHost = window.location.hostname === "127.0.0.1" || window.location.hostname === "localhost";
  if (isLocalHost && window.location.port.startsWith("517")) {
    return "http://127.0.0.1:8080";
  }
  return "";
}

function readStoredAuth(): AuthResponse | null {
  try {
    const raw = window.localStorage.getItem(authStorageKey);
    return raw ? (JSON.parse(raw) as AuthResponse) : null;
  } catch {
    return null;
  }
}
