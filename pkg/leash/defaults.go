package leash

import "time"

// defaultConfig retourne une configuration avec les valeurs de policies/default.yaml.
func defaultConfig() *config {
	return &config{
		maxDuration:          30 * time.Second,
		maxOutputBytes:       1024 * 1024,
		maxCommandsPerScript: 50,
		maxSubshells:         3,
		globalRateLimit:      rateSpec{count: 100, window: time.Minute},
		perSkillRates:        make(map[string]rateSpec),
		allowedBinaries: []string{
			"grep", "sed", "awk", "sort", "uniq", "head", "tail",
			"wc", "tr", "cut", "tee", "cat", "xargs", "date",
			"echo", "printf", "test",
		},
		blockedPatterns: []string{
			"rm -rf", "mkfs", "dd if=", "> /dev/", "chmod 777",
		},
		inheritEnv: false,
		staticEnv: map[string]string{
			"HOME": "/tmp/leash",
			"PATH": "/usr/bin:/bin",
		},
	}
}
