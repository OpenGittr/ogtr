// The ogtr instance-admin service entrypoint. The whole assembly lives in
// backend/adminapp (mirroring backend/server → backend/app); with no options
// behavior is identical to the pre-package main. The directory is
// `backend/admin` (not `backend/internal`) only because `internal/` is Go's
// reserved import-restriction directory name; the component/deployment name
// stays "ogtr-internal".
package main

import "github.com/opengittr/ogtr/backend/adminapp"

func main() {
	adminapp.Run()
}
