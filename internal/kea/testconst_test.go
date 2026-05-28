/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package kea

// Shared string literals used across the kea package tests. Declared here so
// individual test files can reference them without repeating literals (goconst).
const (
	testNameDhcp4        = "kea-dhcp4"
	testLeaseDBName      = "kea_leases"
	testSeverityInfo     = "INFO"
	testHooksLibrariesK  = "hooks-libraries"
	testLeaseDatabaseK   = "lease-database"
	testLoopbackIP       = "127.0.0.1"
	testCtrlSocketPath   = "/run/kea/kea4-ctrl-socket"
	testUserAdmin        = "admin"
	testRoleReadonly     = "readonly"
	testSocketTypeUnix   = "unix"
	testTSIGKeyMD5       = "my-hmac-md5"
	testTSIGKeySHA256    = "my-hmac-sha256"
	testInterfaceEth0    = "eth0"
	testSubnet4          = "192.168.1.0/24"
	testHAModeLB         = "load-balancing"
	testHAModeHotStandby = "hot-standby"
	testServer1URL       = "http://server1:8000/"
	testServer2URL       = "http://server2:8000/"
	testDBHost           = "db.example.com"
	testDBUser           = "kea_user"
	testSharedNetwork    = "office-network"
	testServerName1      = "server1"
	testServerName2      = "server2"
	testRolePrimary      = "primary"
	testRoleStandby      = "standby"
	testPrimaryServer    = "primary-server"
)
