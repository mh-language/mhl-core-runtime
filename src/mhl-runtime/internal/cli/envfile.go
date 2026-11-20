package cli

import (
	"fmt"

	"github.com/mh-language/mhl-core-runtime/internal/features/dotenv"
)

// takeEnvFiles removes every `--env-file <path>` pair from args, returning
// the remaining args and the paths in order.
func takeEnvFiles(args []string) (rest, files []string, err error) {
	for i := 0; i < len(args); i++ {
		if args[i] != "--env-file" {
			rest = append(rest, args[i])
			continue
		}
		if i+1 >= len(args) {
			return nil, nil, fmt.Errorf("--env-file requires a path")
		}
		i++
		files = append(files, args[i])
	}
	return rest, files, nil
}

// loadEnvFiles puts the given env files into the process environment before
// a run or test starts — before extensions are loaded too, so an extension
// process and every agent subprocess inherit them. Without --env-file, the
// working directory's `.env` is loaded when it exists; with it, only the
// named files are (each must exist). A variable already set is never
// overridden (dotenv.Load).
func loadEnvFiles(files []string) error {
	if len(files) == 0 {
		_, err := dotenv.Load([]string{dotenv.DefaultFile}, true)
		return err
	}
	_, err := dotenv.Load(files, false)
	return err
}
