import {
  Activity,
  AlertTriangle,
  BarChart3,
  CheckCircle2,
  ChevronLeft,
  ChevronRight,
  Clock3,
  Copy,
  Crosshair,
  Database,
  FileText,
  FileJson2,
  Gauge,
  History,
  Lightbulb,
  Link2,
  Play,
  Radar,
  RefreshCw,
  Search,
  Shield,
  Swords,
  Timer,
  Video
} from "lucide-react";
import type { ReactNode } from "react";
import { useEffect, useMemo, useRef, useState } from "react";

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
};

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
  vision_service?: VisionServiceHealth;
};

type VisionServiceHealth = {
  configured?: boolean;
  status?: string;
  model?: string;
  mode?: string;
  runtime?: string;
  error?: string;
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
  tags?: string[];
};

type Frame = {
  index: number;
  timestamp_seconds: number;
  path: string;
};

type FrameObservation = {
  index: number;
  timestamp_seconds: number;
  path: string;
  brightness: number;
  contrast: number;
  motion_score: number;
  center_activity: number;
  minimap_signal: number;
  hud_signal: number;
  combat_signal: number;
  phase: string;
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
  tags?: string[];
};

type CoachFocusArea = {
  id: string;
  priority: string;
  category: string;
  title: string;
  detail: string;
  score: number;
  window_ids?: string[];
};

type PracticeTask = {
  id: string;
  title: string;
  detail: string;
  cadence: string;
  tags?: string[];
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
  tags?: string[];
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
  evidence?: Finding["evidence"];
  context?: string[];
  questions?: string[];
  expected_output: string;
  prompt: string;
};

type ModelReviewRun = {
  id: string;
  task_id: string;
  window_id: string;
  status: string;
  model?: string;
  prompt_version: string;
  verdict?: string;
  practice?: string;
  needs_manual_review?: boolean;
  error?: string;
};

type CoachSummary = {
  verdict: string;
  confidence: number;
  coverage_seconds?: number;
  focus_areas?: CoachFocusArea[];
  practice_plan?: PracticeTask[];
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
  phase_profile?: PhaseStat[];
  round_segments?: RoundSegment[];
  gameplay_events?: GameplayEvent[];
  model_review_tasks?: ModelReviewTask[];
  model_review_runs?: ModelReviewRun[];
  frame_observations?: FrameObservation[];
  review_windows?: ReviewWindow[];
  notes?: string[];
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

const ranks = ["all", "iron", "bronze", "silver", "gold", "platinum", "diamond", "ascendant", "immortal", "radiant"];
const evidencePageSize = 24;
const correctionTypes = ["false_detection", "map", "agent", "rank", "round_boundary", "finding_note", "event_note"];

export function App() {
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
  const [analysisJob, setAnalysisJob] = useState<AnalysisJobResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadingReport, setLoadingReport] = useState(false);
  const [analyzing, setAnalyzing] = useState(false);
  const [evaluating, setEvaluating] = useState(false);
  const [savingCorrection, setSavingCorrection] = useState(false);
  const [error, setError] = useState("");
  const [copiedTaskID, setCopiedTaskID] = useState("");
  const [runDuration, setRunDuration] = useState(180);
  const [runFps, setRunFps] = useState("1");
  const [fullVod, setFullVod] = useState(false);
  const [modelReview, setModelReview] = useState(false);
  const [correctionType, setCorrectionType] = useState("false_detection");
  const [correctionTargetID, setCorrectionTargetID] = useState("");
  const [correctionValue, setCorrectionValue] = useState("");
  const [correctionComment, setCorrectionComment] = useState("");
  const [evidencePage, setEvidencePage] = useState(0);
  const [windowKind, setWindowKind] = useState("all");
  const videoRef = useRef<HTMLVideoElement | null>(null);

  const selectedVod = useMemo(() => vods.find((vod) => vod.label === selectedLabel) ?? null, [selectedLabel, vods]);
  const filteredVods = useMemo(() => {
    return vods.filter((vod) => {
      const rankOk = rank === "all" || vod.rank === rank;
      const text = `${vod.label} ${vod.rank} ${vod.title} ${vod.channel}`.toLowerCase();
      const queryOk = !query.trim() || text.includes(query.trim().toLowerCase());
      return rankOk && queryOk;
    });
  }, [query, rank, vods]);

  useEffect(() => {
    void loadBackendHealth();
    void loadVods();
  }, []);

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
    setEvidencePage(0);
  }, [report?.run_id]);

  useEffect(() => {
    setWindowKind("all");
    setCorrectionTargetID("");
    setCorrectionValue("");
    setCorrectionComment("");
  }, [report?.run_id]);

  useEffect(() => {
    if (!selectedLabel || !report?.run_id) {
      setManualCorrections([]);
      setManualCorrectionsPath("");
      return;
    }
    void loadManualCorrections(selectedLabel, report.run_id);
  }, [selectedLabel, report?.run_id]);

  async function loadBackendHealth() {
    try {
      const response = await fetch(apiURL("/api/health"));
      if (!response.ok) {
        return;
      }
      setBackendHealth((await response.json()) as BackendHealth);
    } catch {
      setBackendHealth(null);
    }
  }

  async function loadVods() {
    setLoading(true);
    setError("");
    try {
      const response = await fetch(apiURL("/api/vods"));
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
    setError("");
    try {
      const response = await fetch(apiURL(`/api/reports?vod_label=${encodeURIComponent(label)}`));
      if (!response.ok) {
        throw new Error(await readError(response));
      }
      const payload = (await response.json()) as ReportListResponse;
      setReportHistory(payload.reports);
      if (payload.reports.length === 0) {
        setReport(null);
        return;
      }
      const preferredReport =
        payload.reports.find((item) => item.run_id === options.preferredRunID) ??
        (options.preferGameplay ? payload.reports.find((item) => item.review_window_count > 0 || item.analyzer === "visual-heuristic-gameplay") : undefined) ??
        payload.reports[0];
      await loadReport(label, preferredReport.run_id);
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
    setError("");
    try {
      const response = await fetch(apiURL(`/api/reports/${encodeURIComponent(label)}/${encodeURIComponent(runID)}`));
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
      const response = await fetch(apiURL(`/api/evaluations?vod_label=${encodeURIComponent(label)}`));
      if (!response.ok) {
        return;
      }
      const payload = (await response.json()) as EvaluationListResponse;
      setEvaluationHistory(payload.evaluations);
    } catch {
      setEvaluationHistory([]);
    }
  }

  async function loadEvaluationAnnotations(label: string) {
    try {
      const response = await fetch(apiURL(`/api/evaluation-annotations?vod_label=${encodeURIComponent(label)}`));
      if (!response.ok) {
        return;
      }
      const payload = (await response.json()) as EvaluationAnnotationListResponse;
      setEvaluationAnnotations(payload.annotations);
    } catch {
      setEvaluationAnnotations([]);
    }
  }

  async function loadManualCorrections(label: string, runID: string) {
    try {
      const response = await fetch(apiURL(`/api/corrections?vod_label=${encodeURIComponent(label)}&report_run_id=${encodeURIComponent(runID)}`));
      if (!response.ok) {
        return;
      }
      const payload = (await response.json()) as ManualCorrectionResponse;
      setManualCorrections(payload.corrections);
      setManualCorrectionsPath(payload.json_path);
    } catch {
      setManualCorrections([]);
      setManualCorrectionsPath("");
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

    const currentTime = videoRef.current?.currentTime;
    setSavingCorrection(true);
    setError("");
    try {
      const response = await fetch(apiURL("/api/corrections"), {
        method: "POST",
        headers: { "Content-Type": "application/json" },
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

  async function runEvaluation() {
    if (!selectedVod || !report || evaluating || evaluationAnnotations.length === 0) {
      return;
    }
    const annotation = evaluationAnnotations.find((item) => item.report_run_id && item.report_run_id === report.run_id) ?? evaluationAnnotations[0];
    setEvaluating(true);
    setError("");
    try {
      const response = await fetch(apiURL("/api/evaluation-runs"), {
        method: "POST",
        headers: { "Content-Type": "application/json" },
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

  async function runAnalysis() {
    if (!selectedVod || analyzing) {
      return;
    }
    setAnalyzing(true);
    setAnalysisJob(null);
    setError("");
    try {
      const response = await fetch(apiURL("/api/analysis-runs"), {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          vod_label: selectedVod.label,
          run_id: `ui_${compactTimestamp(new Date())}`,
          fps: runFps,
          image_quality: 3,
          duration_seconds: fullVod ? 0 : runDuration,
          force: true,
          model_review: modelReview && modelReviewAvailable,
          async: true
        })
      });
      if (!response.ok) {
        throw new Error(await readError(response));
      }
      const payload = (await response.json()) as AnalysisJobResponse;
      const analyzedLabel = selectedVod.label;
      setAnalysisJob(payload);
      await pollAnalysisJob(payload.job_id, analyzedLabel);
      setSelectedLabel(analyzedLabel);
    } catch (err) {
      setError(messageFromError(err));
    } finally {
      setAnalyzing(false);
    }
  }

  async function pollAnalysisJob(jobID: string, analyzedLabel: string) {
    for (;;) {
      await sleep(1800);
      const response = await fetch(apiURL(`/api/analysis-runs/${encodeURIComponent(jobID)}`));
      if (!response.ok) {
        throw new Error(await readError(response));
      }
      const job = (await response.json()) as AnalysisJobResponse;
      setAnalysisJob(job);
      if (job.status === "completed") {
        if (job.report) {
          setReport(job.report);
        }
        await loadVods();
        await loadReports(analyzedLabel, { preferredRunID: job.run_id });
        await loadEvaluations(analyzedLabel);
        await loadEvaluationAnnotations(analyzedLabel);
        return;
      }
      if (job.status === "failed") {
        throw new Error(job.error || "Analysis failed");
      }
    }
  }

  const allSampleFrames = report?.sample.frames ?? [];
  const evidencePageCount = Math.max(1, Math.ceil(allSampleFrames.length / evidencePageSize));
  const safeEvidencePage = Math.min(evidencePage, evidencePageCount - 1);
  const evidenceStart = safeEvidencePage * evidencePageSize;
  const evidenceFrames = allSampleFrames.slice(evidenceStart, evidenceStart + evidencePageSize);
  const selectedReportSummary = reportHistory.find((item) => item.run_id === report?.run_id);
  const contactSheetPath = report?.sample.contact_sheet_path || selectedReportSummary?.contact_sheet || "";
  const reviewWindows = report?.gameplay?.review_windows ?? [];
  const roundSegments = report?.gameplay?.round_segments ?? [];
  const gameplayEvents = report?.gameplay?.gameplay_events ?? [];
  const modelReviewTasks = report?.gameplay?.model_review_tasks ?? [];
  const modelReviewRuns = report?.gameplay?.model_review_runs ?? [];
  const selectedEvaluationAnnotation = evaluationAnnotations.find((item) => item.report_run_id && item.report_run_id === report?.run_id) ?? evaluationAnnotations[0] ?? null;
  const reviewWindowKinds = useMemo(() => uniqueWindowKinds(reviewWindows), [reviewWindows]);
  const correctionTargets = useMemo(() => buildCorrectionTargets(report), [report]);
  const visibleReviewWindows = windowKind === "all" ? reviewWindows : reviewWindows.filter((window) => window.kind === windowKind);
  const reportHasGameplay = report ? hasGameplayReview(report) : false;
  const backendMismatch = backendHealth ? (backendHealth.schema_version ?? 1) < 8 || backendHealth.analyzer !== "visual-heuristic-gameplay" : false;
  const modelReviewAvailable = Boolean(backendHealth?.model_review_available);
  const modelReviewConfigured = Boolean(backendHealth?.model_review_configured);
  const visionStatusLabel = modelReviewAvailable
    ? `${backendHealth?.vision_service?.model || "vision-service"} / ${backendHealth?.vision_service?.runtime || backendHealth?.vision_service?.mode || "online"}`
    : modelReviewConfigured
      ? "Vision service offline"
      : "Vision service not configured";
  const modelReviewTitle = modelReviewAvailable
    ? "Run model review through vision-service"
    : modelReviewConfigured
      ? backendHealth?.vision_service?.error || "Vision service is configured but not reachable"
      : "Start vod-web with --vision-url or VISION_SERVICE_URL";

  function seekVideo(seconds: number) {
    const player = videoRef.current;
    if (!player) {
      return;
    }
    player.currentTime = Math.max(0, seconds);
    void player.play().catch(() => undefined);
  }

  async function copyTaskPrompt(task: ModelReviewTask) {
    try {
      await navigator.clipboard.writeText(task.prompt);
      setCopiedTaskID(task.id);
      window.setTimeout(() => setCopiedTaskID((current) => (current === task.id ? "" : current)), 1600);
    } catch {
      setError("Clipboard is unavailable in this browser context.");
    }
  }

  return (
    <main className="app-shell">
      <div className="ambient-grid" />
      <aside className="sidebar">
        <div className="brand-lockup">
          <div className="brand-mark">
            <Crosshair size={22} />
          </div>
          <div>
            <div className="brand-title">VOD COACH</div>
            <div className="brand-subtitle">TACTICAL REVIEW OS</div>
          </div>
        </div>

        <div className="search-box">
          <Search size={16} />
          <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Search VOD, rank, channel" />
        </div>

        <div className="rank-strip" aria-label="Rank filter">
          {ranks.map((rankOption) => (
            <button
              className={rank === rankOption ? "rank-pill active" : "rank-pill"}
              key={rankOption}
              onClick={() => setRank(rankOption)}
              type="button"
            >
              {rankOption}
            </button>
          ))}
        </div>

        <div className="vod-list" aria-label="VOD library">
          {loading ? (
            <SkeletonList />
          ) : (
            filteredVods.map((vod) => (
              <button
                className={vod.label === selectedLabel ? "vod-row active" : "vod-row"}
                key={vod.label}
                onClick={() => setSelectedLabel(vod.label)}
                type="button"
              >
                <span className={`rank-sigil rank-${vod.rank}`}>{vod.rank.slice(0, 3)}</span>
                <span className="vod-row-main">
                  <span className="vod-row-title">{vod.title}</span>
                  <span className="vod-row-meta">
                    {vod.rank} / {vod.duration_text} / {vod.channel}
                  </span>
                </span>
                <span className={vod.local_status === "downloaded" ? "status-dot online" : "status-dot"} />
              </button>
            ))
          )}
        </div>
      </aside>

      <section className="workspace">
        <header className="topbar">
          <div className="topbar-copy">
            <p className="eyebrow">LOCAL MVP</p>
            <h1>{selectedVod?.title ?? "Valorant VOD Coach"}</h1>
          </div>
          <div className="topbar-actions">
            <button className="icon-button" onClick={() => void loadVods()} title="Refresh dataset" type="button">
              <RefreshCw size={18} />
            </button>
            <a className="source-link" href={selectedVod?.source_url ?? "#"} target="_blank" rel="noreferrer">
              <Video size={17} />
              Source
            </a>
          </div>
        </header>

        {error && (
          <div className="error-banner">
            <AlertTriangle size={18} />
            {error}
          </div>
        )}

        {backendMismatch && (
          <div className="compat-banner backend-warning">
            <AlertTriangle size={17} />
            <div>
              <strong>Backend contract mismatch</strong>
              <span>schema {backendHealth?.schema_version ?? 1} / {backendHealth?.analyzer ?? "unknown analyzer"}</span>
            </div>
          </div>
        )}

        <div className="hud-grid">
          <Metric icon={<Database size={18} />} label="Dataset" value={counts ? `${counts.downloaded}/${counts.enabled}` : "..."} detail="downloaded" />
          <Metric icon={<FileJson2 size={18} />} label="Reports" value={counts ? String(counts.reported) : "..."} detail="VODs ready" />
          <Metric icon={<Gauge size={18} />} label="Analyzer" value={report?.metadata.analyzer ?? "baseline"} detail={report?.metadata.mode ?? "local"} />
          <Metric icon={<Timer size={18} />} label="Sample" value={report ? `${report.sample.frame_count}` : "0"} detail="frames" />
        </div>

        <div className="primary-grid">
          <section className="control-deck">
            <div className="panel-heading">
              <div>
                <p className="eyebrow">RUN CONTROL</p>
                <h2>Analysis pipeline</h2>
              </div>
              <span className={analyzing ? "live-chip active" : "live-chip"}>
                <Activity size={14} />
                {analyzing ? "running" : "ready"}
              </span>
            </div>

            <div className="vod-intel">
              <div className="intel-card">
                <span>Rank</span>
                <strong>{selectedVod?.rank ?? "select"}</strong>
              </div>
              <div className="intel-card">
                <span>Duration</span>
                <strong>{selectedVod?.duration_text ?? "--"}</strong>
              </div>
              <div className="intel-card">
                <span>Status</span>
                <strong>{displayLocalStatus(selectedVod)}</strong>
              </div>
            </div>

            <div className="video-stage">
              {loading ? (
                <div className="video-placeholder loading" />
              ) : selectedVod?.video_url ? (
                <video controls preload="metadata" ref={videoRef} src={apiURL(selectedVod.video_url)} />
              ) : (
                <div className="video-placeholder">
                  <Video size={30} />
                  <span>{selectedVod ? "Not downloaded" : "Select VOD"}</span>
                </div>
              )}
            </div>

            <div className="run-controls">
              <label>
                <span>Sample seconds</span>
                <input
                  disabled={fullVod}
                  min={30}
                  max={600}
                  step={30}
                  type="number"
                  value={runDuration}
                  onChange={(event) => setRunDuration(Number(event.target.value))}
                />
              </label>
              <label>
                <span>FPS</span>
                <select value={runFps} onChange={(event) => setRunFps(event.target.value)}>
                  <option value="0.5">0.5</option>
                  <option value="1">1</option>
                  <option value="2">2</option>
                </select>
              </label>
              <label className="toggle-control">
                <input checked={fullVod} onChange={(event) => setFullVod(event.target.checked)} type="checkbox" />
                <span>Full VOD</span>
              </label>
              <label className={modelReviewAvailable ? "toggle-control" : "toggle-control disabled"} title={modelReviewTitle}>
                <input checked={modelReview && modelReviewAvailable} disabled={!modelReviewAvailable} onChange={(event) => setModelReview(event.target.checked)} type="checkbox" />
                <span>Model review</span>
              </label>
              <div className={modelReviewAvailable ? "vision-status online" : "vision-status"} title={modelReviewTitle}>
                <Radar size={14} />
                <span>{visionStatusLabel}</span>
              </div>
              <button className="run-button" disabled={!selectedVod || selectedVod.local_status !== "downloaded" || analyzing} onClick={() => void runAnalysis()} type="button">
                <Play size={18} fill="currentColor" />
                {analyzing ? "Analyzing" : fullVod ? "Run full VOD" : "Run analysis"}
              </button>
            </div>

            {analysisJob && (
              <div className={`analysis-job status-${analysisJob.status}`}>
                <span>{analysisJob.status}</span>
                <strong>{analysisJob.run_id}</strong>
                <small>{analysisJob.message ?? analysisJob.job_id}</small>
              </div>
            )}

            <div className="pipeline-track">
              {["Manifest", "Probe", "Frames", "Clips", "Model", "Report"].map((step, index) => (
                <div className={report || analyzing ? "pipeline-step lit" : "pipeline-step"} key={step}>
                  <span>{index + 1}</span>
                  {step}
                </div>
              ))}
            </div>
          </section>

          <section className="report-panel">
            <div className="panel-heading">
              <div>
                <p className="eyebrow">REPORT</p>
                <h2>{report ? (reportHasGameplay ? "Gameplay review" : "Legacy report") : loadingReport ? "Loading report" : "No report selected"}</h2>
                {report && <small className="panel-subline">Run {report.run_id}</small>}
              </div>
              {report && (
                <span className="success-chip">
                  <CheckCircle2 size={14} />
                  {report.status}
                </span>
              )}
            </div>

            {reportHistory.length > 0 && (
              <div className="report-history">
                <div className="history-title">
                  <History size={15} />
                  Report history
                </div>
                <div className="history-list">
                  {reportHistory.map((item) => (
                    <button
                      className={report?.run_id === item.run_id ? "history-run active" : "history-run"}
                      key={item.run_id}
                      onClick={() => selectedVod && void loadReport(selectedVod.label, item.run_id)}
                      type="button"
                    >
                      <span>{item.run_id}</span>
                      <small>
                        {item.frame_count} frames / {item.review_window_count} windows / {item.round_segment_count || 0} rounds / {item.model_review_run_count || 0}/{item.model_review_task_count || 0} model
                      </small>
                      <small>{item.analyzer ?? `schema ${item.schema_version || 1}`}</small>
                    </button>
                  ))}
                </div>
              </div>
            )}

            {(evaluationHistory.length > 0 || evaluationAnnotations.length > 0) && (
              <div className="quality-panel">
                <div className="history-title">
                  <BarChart3 size={15} />
                  Quality benchmarks
                </div>
                {selectedEvaluationAnnotation && (
                  <div className="quality-actions">
                    <div>
                      <span>{selectedEvaluationAnnotation.label_count} manual labels</span>
                      <small>
                        {selectedEvaluationAnnotation.report_run_id ? `fixture ${selectedEvaluationAnnotation.report_run_id}` : selectedEvaluationAnnotation.path} / tolerance{" "}
                        {formatSeconds(selectedEvaluationAnnotation.tolerance_seconds ?? 6)}
                      </small>
                    </div>
                    <button disabled={!report || evaluating} onClick={() => void runEvaluation()} type="button">
                      <BarChart3 size={14} />
                      {evaluating ? "Running eval" : "Run benchmark"}
                    </button>
                  </div>
                )}
                {evaluationHistory.length > 0 ? (
                  <div className="quality-list">
                    {evaluationHistory.slice(0, 4).map((item) => (
                      <article className="quality-card" key={item.run_id}>
                        <div>
                          <span>{item.run_id}</span>
                          <strong>{Math.round(clamp01(item.f1) * 100)}% F1</strong>
                        </div>
                        <div className="quality-metrics">
                          <small>{Math.round(clamp01(item.precision) * 100)}% precision</small>
                          <small>{Math.round(clamp01(item.recall) * 100)}% recall</small>
                          <small>{item.match_count}/{item.label_count} labels</small>
                        </div>
                        <p>
                          {item.prediction_count} predictions / tolerance {formatSeconds(item.tolerance_seconds)} / report {item.report_run_id}
                        </p>
                        <div className="artifact-actions compact">
                          <a href={artifactURL(item.json_path)} target="_blank" rel="noreferrer">
                            <FileJson2 size={13} />
                            JSON
                          </a>
                          <a href={artifactURL(item.markdown_path)} target="_blank" rel="noreferrer">
                            <FileText size={13} />
                            Markdown
                          </a>
                        </div>
                      </article>
                    ))}
                  </div>
                ) : (
                  <p className="quality-empty">No benchmark runs for this VOD yet.</p>
                )}
              </div>
            )}

            {report ? (
              <>
                <div className="report-stats">
                  <Metric icon={<Shield size={18} />} label="Media" value={formatResolution(report)} detail={report.media.frame_rate ?? "unknown"} compact />
                  <Metric icon={<Swords size={18} />} label="Windows" value={String(report.gameplay?.review_window_count ?? 0)} detail={report.metadata.analyzer} compact />
                  <Metric icon={<Timer size={18} />} label="Rounds" value={String(report.gameplay?.round_segment_count ?? 0)} detail="estimated" compact />
                  <Metric icon={<Clock3 size={18} />} label="Coverage" value={coverageLabel(report)} detail={`${report.sample.fps} fps`} compact />
                </div>

                {!reportHasGameplay && (
                  <div className="compat-banner">
                    <AlertTriangle size={17} />
                    <div>
                      <strong>Legacy baseline report</strong>
                      <span>schema {report.schema_version || 1} / {report.metadata.analyzer || "unknown analyzer"}</span>
                    </div>
                  </div>
                )}

                <div className="artifact-actions">
                  <a href={artifactURL(reportPath(reportHistory, report.run_id, "json"))} target="_blank" rel="noreferrer">
                    <FileJson2 size={15} />
                    JSON
                  </a>
                  <a href={artifactURL(reportPath(reportHistory, report.run_id, "markdown"))} target="_blank" rel="noreferrer">
                    <FileText size={15} />
                    Markdown
                  </a>
                </div>

                <div className="corrections-panel">
                  <div className="history-title">
                    <CheckCircle2 size={15} />
                    Manual corrections
                  </div>
                  <div className="correction-form">
                    <label>
                      <span>Type</span>
                      <select value={correctionType} onChange={(event) => setCorrectionType(event.target.value)}>
                        {correctionTypes.map((type) => (
                          <option key={type} value={type}>
                            {type.replaceAll("_", " ")}
                          </option>
                        ))}
                      </select>
                    </label>
                    <label>
                      <span>Target</span>
                      <select value={correctionTargetID} onChange={(event) => setCorrectionTargetID(event.target.value)}>
                        <option value="">report / general</option>
                        {correctionTargets.map((target) => (
                          <option key={target.id} value={target.id}>
                            {target.label}
                          </option>
                        ))}
                      </select>
                    </label>
                    <label>
                      <span>Value</span>
                      <input value={correctionValue} onChange={(event) => setCorrectionValue(event.target.value)} placeholder="Correct value" />
                    </label>
                    <label className="correction-comment">
                      <span>Comment</span>
                      <textarea value={correctionComment} onChange={(event) => setCorrectionComment(event.target.value)} placeholder="Why this should change" rows={3} />
                    </label>
                    <button disabled={savingCorrection || (!correctionValue.trim() && !correctionComment.trim())} onClick={() => void saveManualCorrection()} type="button">
                      <CheckCircle2 size={15} />
                      {savingCorrection ? "Saving" : "Add correction"}
                    </button>
                  </div>
                  {manualCorrections.length ? (
                    <div className="correction-list">
                      {manualCorrections.slice(-5).reverse().map((correction) => (
                        <article className="correction-card" key={correction.id}>
                          <div>
                            <span>{correction.type.replaceAll("_", " ")}</span>
                            <strong>{correction.target_id || "report"}</strong>
                          </div>
                          {correction.corrected_value ? <p>{correction.corrected_value}</p> : null}
                          {correction.comment ? <small>{correction.comment}</small> : null}
                          <em>
                            {correction.status}
                            {typeof correction.timestamp_seconds === "number" ? ` / ${formatSeconds(correction.timestamp_seconds)}` : ""}
                          </em>
                        </article>
                      ))}
                    </div>
                  ) : (
                    <p className="quality-empty">No corrections for this report.</p>
                  )}
                  {manualCorrectionsPath && (
                    <div className="artifact-actions compact">
                      <a href={artifactURL(manualCorrectionsPath)} target="_blank" rel="noreferrer">
                        <FileJson2 size={13} />
                        Corrections JSON
                      </a>
                    </div>
                  )}
                </div>

                {report.gameplay && (
                  <section className="gameplay-review">
                    {report.gameplay.coach && (
                      <div className="coach-summary">
                        <div className="coach-verdict">
                          <span>Coach</span>
                          <h3>{primaryFocusTitle(report.gameplay.coach)}</h3>
                          <p>{report.gameplay.coach.verdict}</p>
                        </div>
                        <strong>{Math.round(clamp01(report.gameplay.coach.confidence) * 100)}%</strong>
                      </div>
                    )}

                    {report.gameplay.coach?.focus_areas?.length ? (
                      <div className="focus-grid">
                        {report.gameplay.coach.focus_areas.slice(0, 4).map((area) => (
                          <article className={`focus-card priority-${area.priority}`} key={area.id}>
                            <span>
                              {area.priority} / {area.category}
                            </span>
                            <h3>{area.title}</h3>
                            <p>{area.detail}</p>
                            {area.window_ids?.length ? <small>{area.window_ids.join(" / ")}</small> : null}
                          </article>
                        ))}
                      </div>
                    ) : null}

                    <div className="signal-grid">
                      <SignalMeter label="Motion" value={report.gameplay.average_motion_score ?? 0} />
                      <SignalMeter label="Minimap" value={report.gameplay.average_minimap_signal ?? 0} />
                      <SignalMeter label="HUD" value={report.gameplay.average_hud_signal ?? 0} />
                      <SignalMeter label="Combat peak" value={report.gameplay.peak_combat_score ?? 0} />
                    </div>

                    {report.gameplay.phase_profile?.length ? (
                      <div className="phase-profile">
                        {report.gameplay.phase_profile.map((phase) => (
                          <div className="phase-row" key={phase.phase}>
                            <span>{phase.phase}</span>
                            <div>
                              <i style={{ width: `${Math.round(clamp01(phase.ratio) * 100)}%` }} />
                            </div>
                            <strong>{Math.round(clamp01(phase.ratio) * 100)}%</strong>
                          </div>
                        ))}
                      </div>
                    ) : null}

                    {roundSegments.length ? (
                      <div className="round-segments">
                        {roundSegments.map((segment) => (
                          <article className="round-segment" key={segment.round_number}>
                            <div>
                              <span>R{segment.round_number}</span>
                              <strong>{roundRange(segment)}</strong>
                            </div>
                            <p>{segment.summary || "Estimated visual timeline segment."}</p>
                            <small>
                              {Math.round(clamp01(segment.confidence) * 100)}% / {segment.detection_method.replaceAll("_", " ")}
                            </small>
                            {segment.review_window_ids?.length ? <em>{segment.review_window_ids.join(" / ")}</em> : null}
                          </article>
                        ))}
                      </div>
                    ) : null}

                    {gameplayEvents.length ? (
                      <div className="gameplay-event-list">
                        {gameplayEvents.slice(0, 28).map((event) => (
                          <article className={`gameplay-event severity-${event.severity}`} key={event.id}>
                            <div className="gameplay-event-time">
                              <button onClick={() => seekVideo(event.timestamp_seconds)} title="Jump to event" type="button">
                                <Play size={13} fill="currentColor" />
                                {formatSeconds(event.timestamp_seconds)}
                              </button>
                              {event.round_number ? <span>R{event.round_number}</span> : null}
                            </div>
                            <div className="gameplay-event-body">
                              <div>
                                <span>
                                  {event.type.replaceAll("_", " ")} / {event.category}
                                </span>
                                <h3>{event.title}</h3>
                              </div>
                              <p>{event.detail}</p>
                              {event.recommendation ? (
                                <div className="finding-recommendation compact">
                                  <Lightbulb size={14} />
                                  <p>{event.recommendation}</p>
                                </div>
                              ) : null}
                              <div className="gameplay-event-meta">
                                {typeof event.score === "number" ? <strong>{Math.round(clamp01(event.score) * 100)} score</strong> : null}
                                {typeof event.confidence === "number" ? <small>{Math.round(clamp01(event.confidence) * 100)}% confidence</small> : null}
                                {event.window_id ? <small>{event.window_id}</small> : null}
                                {event.start_seconds !== undefined && event.end_seconds !== undefined ? <small>{eventRange(event)}</small> : null}
                              </div>
                              {event.evidence?.length ? (
                                <div className="evidence-links">
                                  {event.evidence.slice(0, 2).map((evidence) => (
                                    <a href={artifactURL(evidence.path)} key={`${event.id}-${evidence.path}-${evidence.frame_index ?? 0}`} target="_blank" rel="noreferrer">
                                      <Link2 size={13} />
                                      {evidenceLabel(evidence)}
                                    </a>
                                  ))}
                                </div>
                              ) : null}
                            </div>
                          </article>
                        ))}
                      </div>
                    ) : null}

                    {modelReviewTasks.length ? (
                      <div className="model-task-list">
                        {modelReviewTasks.map((task) => (
                          <article className={`model-task priority-${task.priority}`} key={task.id}>
                            {(() => {
                              const run = modelReviewRuns.find((item) => item.task_id === task.id);
                              return (
                                <>
                            <div className="model-task-head">
                              <div>
                                <span>
                                  {run?.status ?? task.status} / {task.priority} / {run?.model ?? task.model_hint ?? task.prompt_version}
                                </span>
                                <h3>
                                  {task.round_number ? `R${task.round_number} / ` : ""}
                                  {task.window_id}
                                </h3>
                              </div>
                              <button onClick={() => void copyTaskPrompt(task)} title="Copy model prompt" type="button">
                                <Copy size={14} />
                                {copiedTaskID === task.id ? "Copied" : "Prompt"}
                              </button>
                            </div>
                            <p>{run?.verdict ?? task.questions?.[0] ?? "Review this selected gameplay window with the configured model prompt."}</p>
                            {run?.practice ? <small className="model-practice">{run.practice}</small> : null}
                            <div className="evidence-links">
                              {task.clip_path ? (
                                <a href={artifactURL(task.clip_path)} target="_blank" rel="noreferrer">
                                  <Video size={13} />
                                  Clip
                                </a>
                              ) : null}
                              {task.evidence?.slice(0, 2).map((evidence) => (
                                <a href={artifactURL(evidence.path)} key={`${task.id}-${evidence.path}-${evidence.frame_index ?? 0}`} target="_blank" rel="noreferrer">
                                  <Link2 size={13} />
                                  {evidenceLabel(evidence)}
                                </a>
                              ))}
                            </div>
                                </>
                              );
                            })()}
                          </article>
                        ))}
                      </div>
                    ) : null}

                    {report.gameplay.coach?.practice_plan?.length ? (
                      <div className="practice-list">
                        {report.gameplay.coach.practice_plan.map((task) => (
                          <article className="practice-task" key={task.id}>
                            <span>{task.cadence}</span>
                            <h3>{task.title}</h3>
                            <p>{task.detail}</p>
                          </article>
                        ))}
                      </div>
                    ) : null}

                    {reviewWindows.length > 0 && (
                      <div className="window-filter">
                        {["all", ...reviewWindowKinds].map((kind) => (
                          <button className={windowKind === kind ? "active" : ""} key={kind} onClick={() => setWindowKind(kind)} type="button">
                            {kindLabel(kind)}
                          </button>
                        ))}
                      </div>
                    )}

                    <div className="review-window-list">
                      {visibleReviewWindows.map((window) => (
                        <article className={`review-window severity-${window.severity}`} key={window.id}>
                          <div className="review-window-head">
                            <div>
                              <span>
                                {window.round_number ? `R${window.round_number} / ` : ""}
                                {window.kind.replaceAll("_", " ")} / {windowRange(window)}
                              </span>
                              <h3>{window.title}</h3>
                            </div>
                            <div className="window-actions">
                              {window.clip_path ? (
                                <a className="clip-button" href={artifactURL(window.clip_path)} target="_blank" rel="noreferrer" title="Open review clip">
                                  <Video size={14} />
                                  {window.clip_duration_seconds ? formatSeconds(window.clip_duration_seconds) : "Clip"}
                                </a>
                              ) : null}
                              <button className="seek-button" onClick={() => seekVideo(window.peak_seconds)} type="button" title="Jump to peak">
                                <Play size={14} fill="currentColor" />
                                {formatSeconds(window.peak_seconds)}
                              </button>
                            </div>
                          </div>
                          <p>{window.summary}</p>
                          <div className="finding-recommendation">
                            <Lightbulb size={15} />
                            <p>{window.recommendation}</p>
                          </div>
                          {window.evidence?.length ? (
                            <div className="evidence-links">
                              {window.evidence.map((evidence) => (
                                <a href={artifactURL(evidence.path)} key={`${window.id}-${evidence.path}-${evidence.frame_index ?? 0}`} target="_blank" rel="noreferrer">
                                  <Link2 size={13} />
                                  {evidenceLabel(evidence)}
                                </a>
                              ))}
                            </div>
                          ) : null}
                        </article>
                      ))}
                      {!visibleReviewWindows.length && <div className="muted-line">No gameplay windows selected.</div>}
                    </div>
                  </section>
                )}

                <div className="finding-list">
                  {report.findings.map((finding) => (
                    <article className={`finding severity-${finding.severity}`} key={finding.id}>
                      <div className="finding-head">
                        <div>
                          <span>
                            {finding.severity} / {finding.category}
                          </span>
                          <h3>{finding.title}</h3>
                        </div>
                        {finding.confidence ? <strong>{Math.round(finding.confidence * 100)}%</strong> : null}
                      </div>
                      <p>{finding.detail}</p>
                      {finding.recommendation && (
                        <div className="finding-recommendation">
                          <Lightbulb size={15} />
                          <p>{finding.recommendation}</p>
                        </div>
                      )}
                      {finding.evidence?.length ? (
                        <div className="evidence-links">
                          {finding.evidence.map((evidence) => (
                            <a href={artifactURL(evidence.path)} key={`${finding.id}-${evidence.path}-${evidence.frame_index ?? 0}`} target="_blank" rel="noreferrer">
                              <Link2 size={13} />
                              {evidenceLabel(evidence)}
                            </a>
                          ))}
                        </div>
                      ) : null}
                    </article>
                  ))}
                </div>
              </>
            ) : (
              <div className="empty-state">
                <Radar size={34} />
                <h3>No generated report</h3>
                <p>{selectedVod?.local_status === "downloaded" ? "Run baseline analysis for this VOD." : "This VOD is not downloaded locally."}</p>
              </div>
            )}
          </section>
        </div>

        <div className="secondary-grid">
          <section className="timeline-panel">
            <div className="panel-heading">
              <div>
                <p className="eyebrow">TIMELINE</p>
                <h2>Events</h2>
              </div>
              <BarChart3 size={19} />
            </div>
            <div className="timeline">
              {(report?.timeline ?? []).map((event) => (
                <div className="timeline-event" key={`${event.type}-${event.timestamp_seconds}`}>
                  <span>{formatSeconds(event.timestamp_seconds)}</span>
                  <div>
                    <strong>{event.title}</strong>
                    <small>{event.type}</small>
                    {event.detail ? <p>{event.detail}</p> : null}
                  </div>
                </div>
              ))}
              {!report?.timeline?.length && <div className="muted-line">No timeline events.</div>}
            </div>
          </section>

          <section className="frames-panel">
            <div className="panel-heading">
              <div>
                <p className="eyebrow">EVIDENCE</p>
                <h2>Sample frames</h2>
              </div>
              <div className="evidence-controls">
                <button
                  disabled={safeEvidencePage === 0 || allSampleFrames.length === 0}
                  onClick={() => setEvidencePage((page) => Math.max(0, page - 1))}
                  title="Previous frames"
                  type="button"
                >
                  <ChevronLeft size={17} />
                </button>
                <span>{evidenceRangeLabel(evidenceStart, evidenceFrames.length, allSampleFrames.length)}</span>
                <button
                  disabled={safeEvidencePage >= evidencePageCount - 1 || allSampleFrames.length === 0}
                  onClick={() => setEvidencePage((page) => Math.min(evidencePageCount - 1, page + 1))}
                  title="Next frames"
                  type="button"
                >
                  <ChevronRight size={17} />
                </button>
              </div>
            </div>
            <div className="frame-grid">
              {contactSheetPath && (
                <figure className="contact-sheet-tile">
                  <img src={artifactURL(contactSheetPath)} alt="Contact sheet overview" loading="lazy" />
                  <figcaption>contact sheet</figcaption>
                </figure>
              )}
              {evidenceFrames.map((frame) => (
                <figure className="frame-tile" key={frame.path}>
                  <img src={artifactURL(frame.path)} alt={`Frame ${frame.index}`} loading="lazy" />
                  <figcaption>#{frame.index} / {formatSeconds(frame.timestamp_seconds)}</figcaption>
                </figure>
              ))}
              {!evidenceFrames.length && !contactSheetPath && <div className="muted-line">No frames loaded.</div>}
            </div>
          </section>
        </div>
      </section>
    </main>
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

function SignalMeter(props: { label: string; value: number }) {
  return (
    <div className="signal-meter">
      <div>
        <span>{props.label}</span>
        <strong>{Math.round(clamp01(props.value) * 100)}%</strong>
      </div>
      <div className="signal-track">
        <span style={{ width: `${Math.round(clamp01(props.value) * 100)}%` }} />
      </div>
    </div>
  );
}

function SkeletonList() {
  return (
    <>
      {Array.from({ length: 7 }).map((_, index) => (
        <div className="vod-row skeleton" key={index} />
      ))}
    </>
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

function formatResolution(report: Report) {
  if (!report.media.width || !report.media.height) {
    return "unknown";
  }
  return `${report.media.width}x${report.media.height}`;
}

function coverageLabel(report: Report) {
  if (!report.sample.duration_seconds) {
    return "full";
  }
  return `${Math.round(report.sample.duration_seconds)}s`;
}

function hasGameplayReview(report: Report) {
  return Boolean(report.gameplay && report.metadata.analyzer === "visual-heuristic-gameplay");
}

function primaryFocusTitle(coach: CoachSummary) {
  return coach.focus_areas?.[0]?.title ?? "Full VOD coach summary";
}

function displayLocalStatus(vod: VODItem | null) {
  if (!vod) {
    return "idle";
  }
  if (vod.local_status === "downloaded") {
    return "ready";
  }
  return vod.local_status;
}

function formatSeconds(seconds: number) {
  return `${seconds.toFixed(seconds % 1 === 0 ? 0 : 1)}s`;
}

function windowRange(window: ReviewWindow) {
  return `${formatSeconds(window.start_seconds)}-${formatSeconds(window.end_seconds)}`;
}

function roundRange(segment: RoundSegment) {
  return `${formatSeconds(segment.start_seconds)}-${formatSeconds(segment.end_seconds)}`;
}

function eventRange(event: GameplayEvent) {
  return `${formatSeconds(event.start_seconds ?? event.timestamp_seconds)}-${formatSeconds(event.end_seconds ?? event.timestamp_seconds)}`;
}

function evidenceRangeLabel(start: number, count: number, total: number) {
  if (total === 0) {
    return "0 / 0";
  }
  return `${start + 1}-${start + count} / ${total}`;
}

function uniqueWindowKinds(windows: ReviewWindow[]) {
  return Array.from(new Set(windows.map((window) => window.kind))).sort();
}

function buildCorrectionTargets(report: Report | null) {
  if (!report) {
    return [];
  }
  const targets: Array<{ id: string; label: string }> = [
    { id: `report:${report.run_id}`, label: `report / ${report.run_id}` }
  ];
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
    targets.push({ id: `round:${round.round_number}`, label: compactLabel(`round ${round.round_number} / ${roundRange(round)}`) });
  }
  return targets;
}

function compactLabel(value: string) {
  return value.length > 88 ? `${value.slice(0, 85)}...` : value;
}

function kindLabel(kind: string) {
  if (kind === "all") {
    return "all";
  }
  return kind.replaceAll("_", " ");
}

function clamp01(value: number) {
  if (!Number.isFinite(value)) {
    return 0;
  }
  return Math.min(1, Math.max(0, value));
}

function evidenceLabel(evidence: NonNullable<Finding["evidence"]>[number]) {
  if (evidence.frame_index) {
    return `${evidence.artifact_type} #${evidence.frame_index}`;
  }
  return evidence.artifact_type;
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

function reportPath(history: ReportSummary[], runID: string, kind: "json" | "markdown") {
  const item = history.find((entry) => entry.run_id === runID);
  if (!item) {
    return "";
  }
  return kind === "json" ? item.json_path : item.markdown_path;
}

function apiURL(path: string) {
  const explicitBase = import.meta.env.VITE_API_BASE as string | undefined;
  const base = explicitBase || devBackendBase();
  return `${base}${path}`;
}

function devBackendBase() {
  const isLocalHost = window.location.hostname === "127.0.0.1" || window.location.hostname === "localhost";
  if (isLocalHost && window.location.port.startsWith("517")) {
    return "http://127.0.0.1:8080";
  }
  return "";
}
