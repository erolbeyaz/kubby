package logging

import "github.com/erolbeyaz/kubby/internal/timeutil"

// TimestampLayout mirrors timeutil so log records and API responses never drift apart.
const TimestampLayout = timeutil.TimestampLayout
