// SPDX-License-Identifier: Apache-2.0

package plugin

// withMaxLine lowers the longest accepted incoming line, so a test can send an
// over-long line without allocating 64 MiB. It exists in test builds only.
func withMaxLine(n int) Option { return func(o *options) { o.maxLine = n } }
