package desktop

import "os"

func replaceQueue(source, target string) error { return os.Rename(source, target) }
