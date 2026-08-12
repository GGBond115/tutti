//go:build windows || darwin

package workspace

import workspacefiles "github.com/tutti-os/tutti/packages/workspace/files"

func localFileSearchRequestedKinds(request localFileSearchRequest) (files bool, directories bool) {
	if len(request.IncludeKinds) == 0 {
		files, directories = true, true
	} else {
		for _, kind := range request.IncludeKinds {
			files = files || kind == workspacefiles.EntryKindFile
			directories = directories || kind == workspacefiles.EntryKindDirectory
		}
	}
	if len(request.Filters) > 0 {
		directories = false
	}
	return files, directories
}
