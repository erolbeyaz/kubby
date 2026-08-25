// Package migrations carries the schema Kubby owns.
//
// The SQL files stay exactly where goose expects them on disk; this file only makes them
// part of the binary as well. Copying them elsewhere to be embedded would create a second
// set that drifts from the first, and a schema that disagrees with itself is the worst
// kind of bug to find in production.
package migrations

import "embed"

// FS holds every migration, in order.
//
// Embedded because a deployment is one container: there is no host to run a migration
// tool from, no shell in the image to run it with, and asking an operator to run a
// separate step before the first start is a step they will miss exactly once.
//
//go:embed *.sql
var FS embed.FS
