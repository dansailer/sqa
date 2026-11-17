//go:build bundled_sonar && !(linux && amd64)

package sonarcli

// The bundled_sonar build tag is only supported for linux/amd64 (the only
// release target). This undefined reference makes other targets fail to
// compile with a clear message instead of silently shipping without sonar.
var _ = bundledSonarIsOnlySupportedOnLinuxAmd64
