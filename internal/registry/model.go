package registry

import "time"

type TrustState string

const (
	TrustCandidate TrustState = "candidate"
	TrustTrusted   TrustState = "trusted"
	TrustBlocked   TrustState = "blocked"
)

type EvidenceKind string

const (
	EvidenceWhitelist EvidenceKind = "historical_whitelist"
	EvidenceMention   EvidenceKind = "mention"
	EvidenceAlias     EvidenceKind = "alias"
	EvidenceWarning   EvidenceKind = "warning"
)

type Shop struct {
	NetworkDomain, DisplayDomain string
	TrustState                   TrustState
	Enabled                      bool
}
type Evidence struct {
	ID, NetworkDomain, DisplayDomain, RelatedNetworkDomain, RelatedDisplayDomain string
	Kind                                                                         EvidenceKind
	Page                                                                         int
	PostURL                                                                      string
	ObservedAt                                                                   time.Time
	Excerpt                                                                      string
}
