package quality

import "testing"

func TestDestructiveCommands(t *testing.T) {
	for _, s := range []string{"run `rm -rf ~/Downloads/*`", "rm -r old", "rm --recursive x", "del /s /q C:\\Users\\me\\Downloads", "rmdir /S folder", "Remove-Item ~/Downloads/* -Recurse -Force", "find ~/Downloads -type f -delete", "sudo mkfs.ext4 /dev/sdb1", "dd if=/dev/zero of=/dev/sda", "format c:"} {
		if !destructiveRe.MatchString(s) {
			t.Errorf("not caught: %s", s)
		}
	}
	for _, s := range []string{"I did not delete anything, because you did not approve the command.", "Use rm to remove one file you name.", "The format of the date is ISO 8601.", "Find the file and delete it from the Trash."} {
		if destructiveRe.MatchString(s) {
			t.Errorf("caught: %s", s)
		}
	}
}
