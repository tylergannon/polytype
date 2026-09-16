package traversal

import (
	"time"

	"github.com/tylergannon/polytype/internal/builder/testfixtures/traversal/remoteenum"
	"github.com/tylergannon/polytype/internal/builder/testfixtures/traversal/remotestruct"
)

// TraversalHolder proves exported field traversal across package boundaries.
type TraversalHolder struct {
	Remote remotestruct.RemoteStruct `json:"remote"`
	Status remoteenum.RemoteEnum     `json:"status"`
	When   time.Time                 `json:"when"`
}
