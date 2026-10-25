// Package ftp là client FTP tối giản (RFC 959) chỉ phục vụ tải file: đăng
// nhập, chế độ nhị phân, lấy kích thước, tải theo offset và mở nhiều kết nối
// dữ liệu song song.
//
// Chỉ hỗ trợ những gì engine cần: PASV/EPSV, SIZE, REST + RETR. Không có TLS
// (FTPS), không đổi thư mục, không MLSD.
package ftp

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Timeout mặc định cho một lần đọc phản hồi điều khiển.
const DefaultTimeout = 30 * time.Second

var (
	// ErrScheme: URL không phải ftp.
	ErrScheme = errors.New("ftp: URL phải có scheme ftp")
	// ErrBadReply: phản hồi không đúng định dạng.
	ErrBadReply = errors.New("ftp: phản hồi không hợp lệ")
	// ErrStatus: server trả mã lỗi; mang theo mã và thông điệp.
	ErrStatus = errors.New("ftp: lệnh thất bại")
	// ErrNoPassive: server không có PASV/EPSV.
	ErrNoPassive = errors.New("ftp: server không hỗ trợ kết nối thụ động")
)

// Addr là đích kết nối đã tách từ URL.
type Addr struct {
	Host     string // host:port, mặc định cổng 21
	User     string
	Password string
	Path     string // đường dẫn file, đã unescape
}

// ParseURL tách URL ftp thành thông tin kết nối.
func ParseURL(raw string) (*Addr, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "ftp" {
		return nil, ErrScheme
	}
	host := u.Host
	if _, _, err := net.SplitHostPort(host); err != nil {
		host = net.JoinHostPort(host, "21")
	}
	a := &Addr{Host: host, User: "anonymous", Password: "anonymous@", Path: u.Path}
	if u.User != nil {
		a.User = u.User.Username()
		if p, ok := u.User.Password(); ok {
			a.Password = p
		}
	}
	if a.Path == "" || a.Path == "/" {
		return nil, fmt.Errorf("ftp: URL thiếu đường dẫn file: %q", raw)
	}
	return a, nil
}

// StatusError mô tả một phản hồi lỗi từ server.
type StatusError struct {
	Code int
	Msg  string
	line int
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("%v: %d %s (dòng %d)", ErrStatus, e.Code, e.Msg, e.line)
}

// Client là một phiên điều khiển FTP.
type Client struct {
	conn    net.Conn
	br      *bufio.Reader
	timeout time.Duration
	line    int
}

// Dial mở kết nối điều khiển tới host trong a và chờ lời chào.
func Dial(ctx context.Context, a *Addr, timeout time.Duration) (*Client, error) {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", a.Host)
	if err != nil {
		return nil, err
	}
	c := &Client{conn: conn, br: bufio.NewReader(conn), timeout: timeout}
	if err := c.setDeadline(); err != nil {
		conn.Close()
		return nil, err
	}
	code, msg, err := c.reply()
	if err != nil {
		conn.Close()
		return nil, err
	}
	if code != 220 {
		conn.Close()
		return nil, &StatusError{Code: code, Msg: msg, line: c.line}
	}
	return c, nil
}

func (c *Client) setDeadline() error {
	if c.timeout <= 0 {
		return nil
	}
	return c.conn.SetDeadline(time.Now().Add(c.timeout))
}

// reply đọc một phản hồi, xử lý cả dạng nhiều dòng "123-…".
func (c *Client) reply() (int, string, error) {
	var code int
	var msg strings.Builder
	for {
		if err := c.setDeadline(); err != nil {
			return 0, "", err
		}
		line, err := c.br.ReadString('\n')
		if err != nil {
			return 0, "", err
		}
		c.line++
		line = strings.TrimRight(line, "\r\n")
		if len(line) < 3 {
			return 0, "", ErrBadReply
		}
		n, err := strconv.Atoi(line[:3])
		if err != nil {
			return 0, "", ErrBadReply
		}
		if code == 0 {
			code = n
		} else if n != code {
			return 0, "", ErrBadReply
		}
		if len(line) > 3 {
			if msg.Len() > 0 {
				msg.WriteByte('\n')
			}
			msg.WriteString(line[4:])
		}
		// Dòng cuối: dấu cách ở vị trí 4, hoặc dòng ngắn chỉ có mã.
		if len(line) == 3 || line[3] == ' ' {
			return code, msg.String(), nil
		}
	}
}

// do gửi một lệnh và trả về mã phản hồi.
func (c *Client) do(format string, args ...any) (int, string, error) {
	cmd := fmt.Sprintf(format, args...)
	if err := c.setDeadline(); err != nil {
		return 0, "", err
	}
	if _, err := c.conn.Write([]byte(cmd + "\r\n")); err != nil {
		return 0, "", err
	}
	return c.reply()
}

// expect gửi lệnh và đòi một trong các mã mong đợi.
func (c *Client) expect(cmd string, want ...int) (string, error) {
	code, msg, err := c.do("%s", cmd)
	if err != nil {
		return "", err
	}
	for _, w := range want {
		if code == w {
			return msg, nil
		}
	}
	return "", &StatusError{Code: code, Msg: msg, line: c.line}
}

// Login đăng nhập; gọi ngay sau Dial. Mã 230 ở bước USER nghĩa là không cần PASS.
func (c *Client) Login(user, pass string) error {
	code, msg, err := c.do("USER %s", user)
	if err != nil {
		return err
	}
	switch code {
	case 230:
		return nil
	case 331:
		_, err := c.expect("PASS "+pass, 230, 202)
		return err
	default:
		return &StatusError{Code: code, Msg: msg, line: c.line}
	}
}

// Binary chuyển phiên sang chế độ nhị phân (TYPE I).
func (c *Client) Binary() error {
	_, err := c.expect("TYPE I", 200)
	return err
}

// Size lấy kích thước file qua SIZE. Trả về lỗi nếu server không hỗ trợ.
func (c *Client) Size(path string) (int64, error) {
	msg, err := c.expect("SIZE "+path, 213)
	if err != nil {
		return -1, err
	}
	n, err := strconv.ParseInt(strings.TrimSpace(msg), 10, 64)
	if err != nil {
		return -1, ErrBadReply
	}
	return n, nil
}

// Restart đặt offset bắt đầu cho lần RETR kế tiếp.
func (c *Client) Restart(offset int64) error {
	if offset <= 0 {
		return nil
	}
	_, err := c.expect(fmt.Sprintf("REST %d", offset), 350)
	return err
}

// Quit kết thúc phiên lịch sự.
func (c *Client) Quit() error {
	_, _, err := c.do("QUIT")
	return err
}

// Close đóng kết nối điều khiển.
func (c *Client) Close() error {
	if c.conn == nil {
		return nil
	}
	err := c.conn.Close()
	c.conn = nil
	return err
}

// passive mở kết nối dữ liệu thụ động qua EPSV rồi PASV.
func (c *Client) passive() (net.Conn, error) {
	if addr, err := c.epsv(); err == nil {
		return c.dialData(addr)
	}
	addr, err := c.pasv()
	if err != nil {
		return nil, err
	}
	return c.dialData(addr)
}

func (c *Client) dialData(addr string) (net.Conn, error) {
	d := net.Dialer{Timeout: c.timeout}
	conn, err := d.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	if c.timeout > 0 {
		_ = conn.SetDeadline(time.Now().Add(c.timeout))
	}
	return conn, nil
}

func (c *Client) epsv() (string, error) {
	msg, err := c.expect("EPSV", 229)
	if err != nil {
		return "", err
	}
	open := strings.IndexByte(msg, '(')
	closeIdx := strings.IndexByte(msg, ')')
	if open < 0 || closeIdx < open {
		return "", ErrBadReply
	}
	inner := msg[open+1 : closeIdx]
	parts := strings.Split(inner, string(inner[0]))
	if len(parts) < 4 {
		return "", ErrBadReply
	}
	port := parts[len(parts)-2]
	host, _, _ := net.SplitHostPort(c.conn.RemoteAddr().String())
	return net.JoinHostPort(host, port), nil
}

func (c *Client) pasv() (string, error) {
	msg, err := c.expect("PASV", 227)
	if err != nil {
		return "", ErrNoPassive
	}
	open := strings.IndexByte(msg, '(')
	closeIdx := strings.LastIndexByte(msg, ')')
	if open < 0 || closeIdx < open {
		return "", ErrBadReply
	}
	nums := strings.Split(msg[open+1:closeIdx], ",")
	if len(nums) != 6 {
		return "", ErrBadReply
	}
	host := strings.Join(nums[:4], ".")
	p1, err1 := strconv.Atoi(strings.TrimSpace(nums[4]))
	p2, err2 := strconv.Atoi(strings.TrimSpace(nums[5]))
	if err1 != nil || err2 != nil {
		return "", ErrBadReply
	}
	// Server sau NAT có thể trả IP riêng; nếu là 0.0.0.0 thì dùng IP điều khiển.
	if host == "0.0.0.0" {
		host, _, _ = net.SplitHostPort(c.conn.RemoteAddr().String())
	}
	return net.JoinHostPort(host, strconv.Itoa(p1*256+p2)), nil
}

// Retr mở kết nối dữ liệu và bắt đầu tải path từ offset, giới hạn tối đa
// length byte (length < 0 là không giới hạn). Reader trả về tự đóng kết nối
// dữ liệu khi hết dữ liệu hoặc khi Close được gọi.
func (c *Client) Retr(path string, offset, length int64) (io.ReadCloser, error) {
	if err := c.Restart(offset); err != nil {
		return nil, err
	}
	data, err := c.passive()
	if err != nil {
		return nil, err
	}
	code, msg, err := c.do("RETR %s", path)
	if err != nil {
		data.Close()
		return nil, err
	}
	if code != 150 && code != 125 {
		data.Close()
		return nil, &StatusError{Code: code, Msg: msg, line: c.line}
	}
	if length >= 0 {
		return &limitedConn{Conn: data, remaining: length}, nil
	}
	return data, nil
}

// limitedConn đọc tối đa remaining byte rồi tự đóng, bỏ qua phần thừa.
type limitedConn struct {
	net.Conn
	remaining int64
	closed    bool
}

func (l *limitedConn) Read(p []byte) (int, error) {
	if l.remaining <= 0 {
		l.closeOnce()
		return 0, io.EOF
	}
	if int64(len(p)) > l.remaining {
		p = p[:l.remaining]
	}
	n, err := l.Conn.Read(p)
	l.remaining -= int64(n)
	if err == io.EOF && l.remaining > 0 {
		// Server đóng sớm: vẫn trả n byte đã đọc, lần sau trả EOF.
		err = nil
		if n == 0 {
			l.closeOnce()
			return 0, io.ErrUnexpectedEOF
		}
	}
	if l.remaining <= 0 {
		l.closeOnce()
	}
	return n, err
}

func (l *limitedConn) Close() error { return l.closeOnce() }

func (l *limitedConn) closeOnce() error {
	if l.closed {
		return nil
	}
	l.closed = true
	return l.Conn.Close()
}

// OpenRange mở một kết nối mới, đăng nhập và trả reader cho [start, end].
// Dùng cho tải song song: mỗi chunk một kết nối riêng.
func OpenRange(ctx context.Context, rawURL string, start, end int64, timeout time.Duration) (io.ReadCloser, error) {
	a, err := ParseURL(rawURL)
	if err != nil {
		return nil, err
	}
	c, err := Dial(ctx, a, timeout)
	if err != nil {
		return nil, err
	}
	if err := c.Login(a.User, a.Password); err != nil {
		c.Close()
		return nil, err
	}
	if err := c.Binary(); err != nil {
		c.Quit()
		c.Close()
		return nil, err
	}
	length := int64(-1)
	if end >= start {
		length = end - start + 1
	}
	rc, err := c.Retr(a.Path, start, length)
	if err != nil {
		c.Quit()
		c.Close()
		return nil, err
	}
	return &sessionReader{rc: rc, c: c}, nil
}

// sessionReader đóng cả phiên điều khiển khi reader bị đóng.
type sessionReader struct {
	rc io.ReadCloser
	c  *Client
}

func (s *sessionReader) Read(p []byte) (int, error) { return s.rc.Read(p) }

func (s *sessionReader) Close() error {
	err := s.rc.Close()
	_ = s.c.Quit()
	_ = s.c.Close()
	return err
}

// Probe lấy kích thước file qua SIZE (dùng cho URL ftp).
func Probe(ctx context.Context, rawURL string, timeout time.Duration) (int64, error) {
	a, err := ParseURL(rawURL)
	if err != nil {
		return -1, err
	}
	c, err := Dial(ctx, a, timeout)
	if err != nil {
		return -1, err
	}
	defer c.Close()
	if err := c.Login(a.User, a.Password); err != nil {
		return -1, err
	}
	defer c.Quit()
	return c.Size(a.Path)
}
