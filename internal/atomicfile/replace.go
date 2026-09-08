// Package atomicfile provides filesystem operations used when a completed
// temporary file must replace a destination.
package atomicfile

// Replace atomically moves source over destination where the host filesystem
// supports replacement. The source is consumed on success. Callers retain
// ownership of cleanup when this function returns an error.
func Replace(source, destination string) error {
	return replace(source, destination)
}
