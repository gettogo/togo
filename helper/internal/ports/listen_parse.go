package ports

import (
	"bufio"
	"encoding/hex"
	"net"
	"strconv"
	"strings"
)

// parseLsofF reads `lsof -F pcn` output: a "p" line starts a process, a "c"
// line names its command, and each "n" line is one listening socket as
// "addr:port" with "*" for a wildcard and brackets around IPv6.
func parseLsofF(out string) []Listener {
	var (
		found   []Listener
		pid     int
		command string
	)
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := sc.Text()
		if len(line) < 2 {
			continue
		}
		switch line[0] {
		case 'p':
			pid, _ = strconv.Atoi(line[1:])
			command = ""
		case 'c':
			command = line[1:]
		case 'n':
			addr, port, ok := splitHostPort(line[1:])
			if ok && pid > 0 {
				found = append(found, Listener{Port: port, Address: addr, PID: pid, Command: command})
			}
		}
	}
	return found
}

func splitHostPort(s string) (string, int, bool) {
	i := strings.LastIndexByte(s, ':')
	if i < 0 {
		return "", 0, false
	}
	port, err := strconv.Atoi(s[i+1:])
	if err != nil || port < 1 || port > 65535 {
		return "", 0, false
	}
	return strings.Trim(s[:i], "[]"), port, true
}

// procSocket is one LISTEN row of /proc/net/tcp or /proc/net/tcp6.
type procSocket struct {
	Address string
	Port    int
	Inode   uint64
}

const procStateListen = "0A"

// parseProcNetTCP keeps the rows in the LISTEN state. Addresses are hex in
// host byte order, one 32-bit word at a time, so each word is reversed on
// little-endian machines.
func parseProcNetTCP(content string) []procSocket {
	var socks []procSocket
	sc := bufio.NewScanner(strings.NewReader(content))
	sc.Scan() // header
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 10 || f[3] != procStateListen {
			continue
		}
		addr, port, ok := parseProcAddr(f[1])
		inode, err := strconv.ParseUint(f[9], 10, 64)
		if !ok || err != nil || inode == 0 {
			continue
		}
		socks = append(socks, procSocket{Address: addr, Port: port, Inode: inode})
	}
	return socks
}

func parseProcAddr(s string) (string, int, bool) {
	host, portHex, ok := strings.Cut(s, ":")
	if !ok {
		return "", 0, false
	}
	port, err := strconv.ParseUint(portHex, 16, 16)
	raw, herr := hex.DecodeString(host)
	if err != nil || herr != nil || (len(raw) != 4 && len(raw) != 16) || port == 0 {
		return "", 0, false
	}
	for i := 0; i < len(raw); i += 4 {
		raw[i], raw[i+1], raw[i+2], raw[i+3] = raw[i+3], raw[i+2], raw[i+1], raw[i]
	}
	return net.IP(raw).String(), int(port), true
}
