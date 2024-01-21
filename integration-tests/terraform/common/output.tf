// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: MIT

output "testing_id" {
  value = random_id.testing_id.hex
}

output "cwa_iam_role" {
  value = "Admin-Windows"
}

output "vpc_security_group" {
  value = "vpc_security_group"
}
