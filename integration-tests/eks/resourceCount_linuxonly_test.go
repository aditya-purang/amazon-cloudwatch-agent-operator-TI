// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

//go:build linuxonly
// +build linuxonly

package eks_addon

const (
	serviceCountLinux     = 4
	serviceCountWindows   = 0
	daemonsetCountLinux   = 2
	daemonsetCountWindows = 0
	podCountLinux         = 3
	podCountWindows       = 0
)
