package bot

import (
	"testing"
)
import "github.com/stretchr/testify/assert"

func TestUnquote(t *testing.T) {
	assert.Equal(t, "line 1\nline\t2\nline 3", unquote("line 1\\nline\\t2\\nline \\x33"))
}

func TestAddCursor(t *testing.T) {
	before := `root@89cee82bafd5:/# ls
bin   dev  home  lib32  libx32  mnt  proc  run   srv  tmp  var
boot  etc  lib   lib64  media   opt  root  sbin  sys  usr
root@89cee82bafd5:/# ls -al


`
	expected := `root@89cee82bafd5:/# ls
bin   dev  home  lib32  libx32  mnt  proc  run   srv  tmp  var
boot  etc  lib   lib64  media   opt  root  sbin  sys  usr
root@89cee82bafd5:/# ls -al█


`
	actual := addCursor(before, 27, 3)
	assert.Equal(t, expected, actual)
}

func TestAddExitedMessageWithWhitespaces(t *testing.T) {
	before := `root@89cee82bafd5:/# ls
bin   dev  home  lib32  libx32  mnt  proc  run   srv  tmp  var
boot  etc  lib   lib64  media   opt  root  sbin  sys  usr
root@89cee82bafd5:/# ls -al



`
	expected := `root@89cee82bafd5:/# ls
bin   dev  home  lib32  libx32  mnt  proc  run   srv  tmp  var
boot  etc  lib   lib64  media   opt  root  sbin  sys  usr
root@89cee82bafd5:/# ls -al


(REPL exited.)
`
	actual := addExitedMessage(before)
	assert.Equal(t, expected, actual)
}

func TestAddExitedMessageWithoutWhitespaces(t *testing.T) {
	before := `root@89cee82bafd5:/# ls
bin   dev  home  lib32  libx32  mnt  proc  run   srv  tmp  var
boot  etc  lib   lib64  media   opt  root  sbin  sys  usr
root@89cee82bafd5:/# ls -al
`
	expected := `root@89cee82bafd5:/# ls
bin   dev  home  lib32  libx32  mnt  proc  run   srv  tmp  var
boot  etc  lib   lib64  media   opt  root  sbin  sys  usr
root@89cee82bafd5:/# ls -al

(REPL exited.)
`
	actual := addExitedMessage(before)
	assert.Equal(t, expected, actual)
}

func TestExpandWindow(t *testing.T) {
	before := `root@89cee82bafd5:/# ls
bin   dev  home  lib32  libx32  mnt  proc  run   srv  tmp  var
boot  etc  lib   lib64  media   opt  root  sbin  sys  usr
root@89cee82bafd5:/# ls -al





`
	expected := `root@89cee82bafd5:/# ls
bin   dev  home  lib32  libx32  mnt  proc  run   srv  tmp  var
boot  etc  lib   lib64  media   opt  root  sbin  sys  usr
root@89cee82bafd5:/# ls -al





.
`
	actual := expandWindow(before)
	assert.Equal(t, expected, actual)
}

func TestExpandWindowDontExpand(t *testing.T) {
	before := `This window
does not need to be expanded.
`
	expected := `This window
does not need to be expanded.
`
	actual := expandWindow(before)
	assert.Equal(t, expected, actual)
}
