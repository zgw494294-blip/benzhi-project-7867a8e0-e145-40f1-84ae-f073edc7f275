package domain

import "time"

type ProductionState string

const (
	StateDraft            ProductionState = "draft"
	StatePendingAnalysis  ProductionState = "pending_analysis"
	StatePendingRehearsal ProductionState = "pending_rehearsal"
	StatePendingReview    ProductionState = "pending_review"
	StateRemediation      ProductionState = "remediation"
	StateFrozen           ProductionState = "frozen"
	StateReleased         ProductionState = "released"
)

type ProductionFile struct {
	ID                   string          `json:"id"`
	Title                string          `json:"title"`
	VenueName            string          `json:"venueName"`
	StageWidthMM         float64         `json:"stageWidthMM"`
	StageDepthMM         float64         `json:"stageDepthMM"`
	PerformanceStartsAt  time.Time       `json:"performanceStartsAt"`
	TechnicalDirectorID  string          `json:"technicalDirectorID"`
	State                ProductionState `json:"state"`
	Revision             int64           `json:"revision"`
	LastEditorID         string          `json:"lastEditorID"`
	LastAnalysisRevision int64           `json:"lastAnalysisRevision"`
	CreatedAt            time.Time       `json:"createdAt"`
	UpdatedAt            time.Time       `json:"updatedAt"`
}

type RiggingElement struct {
	ID              string  `json:"id"`
	ProductionID    string  `json:"productionID"`
	Kind            string  `json:"kind"`
	Label           string  `json:"label"`
	XMM             float64 `json:"xMM"`
	YMM             float64 `json:"yMM"`
	ZMM             float64 `json:"zMM"`
	RatedLoadKG     float64 `json:"ratedLoadKG"`
	AppliedLoadKG   float64 `json:"appliedLoadKG"`
	SafetyFactor    float64 `json:"safetyFactor"`
	ParentElementID string  `json:"parentElementID,omitempty"`
	Revision        int64   `json:"revision"`
}

type MotionCue struct {
	ID                 string   `json:"id"`
	ProductionID       string   `json:"productionID"`
	SequenceNo         int      `json:"sequenceNo"`
	ElementID          string   `json:"elementID"`
	StartsAtMS         int64    `json:"startsAtMS"`
	DurationMS         int64    `json:"durationMS"`
	TargetZMM          float64  `json:"targetZMM"`
	SpeedMMPerSecond   float64  `json:"speedMMPerSecond"`
	OperatorGroup      string   `json:"operatorGroup"`
	PreconditionCueIDs []string `json:"preconditionCueIDs"`
	ExclusionZone      bool     `json:"exclusionZone"`
	PersonnelInZone    bool     `json:"personnelInZone"`
	MinimumClearanceMM float64  `json:"minimumClearanceMM"`
	Revision           int64    `json:"revision"`
}

type SafetyFinding struct {
	ID               string             `json:"id"`
	ProductionID     string             `json:"productionID"`
	AnalysisRevision int64              `json:"analysisRevision"`
	RuleCode         string             `json:"ruleCode"`
	Severity         string             `json:"severity"`
	ElementIDs       []string           `json:"elementIDs"`
	CueIDs           []string           `json:"cueIDs"`
	Explanation      string             `json:"explanation"`
	EvidenceValues   map[string]float64 `json:"evidenceValues"`
	ResolutionState  string             `json:"resolutionState"`
	RemediationNote  string             `json:"remediationNote,omitempty"`
}

// AnalysisBatch 是一次不可变的确定性分析结果。Snapshot.Findings 仍表示最新批次，
// 历史批次用于追溯与只读对比。
type AnalysisBatch struct {
	ID               string          `json:"id"`
	ProductionID     string          `json:"productionID"`
	AnalysisRevision int64           `json:"analysisRevision"`
	RuleVersion      string          `json:"ruleVersion"`
	ExecutedAt       time.Time       `json:"executedAt"`
	Findings         []SafetyFinding `json:"findings"`
}

type FindingRemediation struct {
	ID                       string    `json:"id"`
	ProductionID             string    `json:"productionID"`
	FindingID                string    `json:"findingID"`
	FindingKey               string    `json:"findingKey"`
	OwnerID                  string    `json:"ownerID"`
	Measure                  string    `json:"measure"`
	TargetCueID              string    `json:"targetCueID"`
	BaselineAnalysisRevision int64     `json:"baselineAnalysisRevision"`
	Status                   string    `json:"status"`
	LatestFindingID          string    `json:"latestFindingID,omitempty"`
	ResolvedAnalysisRevision int64     `json:"resolvedAnalysisRevision,omitempty"`
	ClosingEvidenceID        string    `json:"closingEvidenceID,omitempty"`
	CreatedAt                time.Time `json:"createdAt"`
	ClosedAt                 time.Time `json:"closedAt,omitempty"`
}

type RehearsalEvidence struct {
	ID                   string    `json:"id"`
	ProductionID         string    `json:"productionID"`
	CueID                string    `json:"cueID"`
	RunNumber            int       `json:"runNumber"`
	PerformedAt          time.Time `json:"performedAt"`
	OperatorID           string    `json:"operatorID"`
	Result               string    `json:"result"`
	MeasuredDeviationMM  float64   `json:"measuredDeviationMM"`
	IncidentDescription  string    `json:"incidentDescription,omitempty"`
	AttachmentName       string    `json:"attachmentName,omitempty"`
	AttachmentSHA256     string    `json:"attachmentSHA256,omitempty"`
	SupersedesEvidenceID string    `json:"supersedesEvidenceID,omitempty"`
	AnalysisRevision     int64     `json:"analysisRevision"`
}

type ReviewDecision struct {
	ID                 string            `json:"id"`
	ReviewerID         string            `json:"reviewerID"`
	Decision           string            `json:"decision"`
	Comments           map[string]string `json:"comments"`
	ProductionRevision int64             `json:"productionRevision"`
	DecidedAt          time.Time         `json:"decidedAt"`
}

type ReviewResponseItem struct {
	ID                      string    `json:"id"`
	ReviewID                string    `json:"reviewID"`
	FindingID               string    `json:"findingID"`
	Comment                 string    `json:"comment"`
	OwnerID                 string    `json:"ownerID"`
	Status                  string    `json:"status"`
	Response                string    `json:"response,omitempty"`
	RelatedRevision         int64     `json:"relatedRevision,omitempty"`
	RelatedAnalysisRevision int64     `json:"relatedAnalysisRevision,omitempty"`
	RelatedEvidenceID       string    `json:"relatedEvidenceID,omitempty"`
	RespondedBy             string    `json:"respondedBy,omitempty"`
	RespondedAt             time.Time `json:"respondedAt,omitempty"`
}

type FrozenManifest struct {
	ProductionID string    `json:"productionID"`
	Revision     int64     `json:"revision"`
	SHA256       string    `json:"sha256"`
	Canonical    string    `json:"canonical"`
	FrozenAt     time.Time `json:"frozenAt"`
}

type ReleaseCredential struct {
	ID                       string    `json:"id"`
	ProductionID             string    `json:"productionID"`
	FrozenRevision           int64     `json:"frozenRevision"`
	ManifestSHA256           string    `json:"manifestSHA256"`
	ApprovedBy               string    `json:"approvedBy"`
	IssuedBy                 string    `json:"issuedBy"`
	ValidPerformanceStartsAt time.Time `json:"validPerformanceStartsAt"`
	IssuedAt                 time.Time `json:"issuedAt"`
	VerificationCode         string    `json:"verificationCode"`
	Status                   string    `json:"status"`
	ReplacesCredentialID     string    `json:"replacesCredentialID,omitempty"`
}

type CredentialStatusEvent struct {
	ID            string    `json:"id"`
	CredentialID  string    `json:"credentialID"`
	Status        string    `json:"status"`
	Reason        string    `json:"reason"`
	ActorID       string    `json:"actorID"`
	ReplacementID string    `json:"replacementID,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
}

type Snapshot struct {
	Production       ProductionFile          `json:"production"`
	Elements         []RiggingElement        `json:"elements"`
	Cues             []MotionCue             `json:"cues"`
	Findings         []SafetyFinding         `json:"findings"`
	Evidence         []RehearsalEvidence     `json:"evidence"`
	Reviews          []ReviewDecision        `json:"reviews"`
	Manifest         *FrozenManifest         `json:"manifest,omitempty"`
	Credentials      []ReleaseCredential     `json:"credentials"`
	AnalysisBatches  []AnalysisBatch         `json:"analysisBatches"`
	Remediations     []FindingRemediation    `json:"remediations"`
	ReviewItems      []ReviewResponseItem    `json:"reviewItems"`
	CredentialEvents []CredentialStatusEvent `json:"credentialEvents"`
}
