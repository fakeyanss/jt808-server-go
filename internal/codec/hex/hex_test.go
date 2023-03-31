package hex

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStr2Byte(t *testing.T) {
	type args struct {
		str string
	}
	tests := []struct {
		name string
		args args
		want []byte
	}{
		{
			name: "case1: short str",
			args: args{str: "123456789012"},
			want: []byte{0x12, 0x34, 0x56, 0x78, 0x90, 0x12},
		},
		{
			name: "case2: long str",
			args: args{str: "000000000002080301CD779E0728C032003C0000008F230125145158"},
			want: []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0x08, 0x03, 0x01, 0xCD, 0x77, 0x9E, 0x07, 0x28, 0xC0, 0x32, 0x00, 0x3C, 0x00, 0x00, 0x00, 0x8F,
				0x23, 0x01, 0x25, 0x14, 0x51, 0x58},
		},
		{
			name: "case3: str of odd length",
			args: args{str: "12345678901"},
			want: []byte{0x12, 0x34, 0x56, 0x78, 0x90},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Str2Byte(tt.args.str)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestByte2Str(t *testing.T) {
	type args struct {
		src []byte
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "case1: byte to str",
			args: args{src: []byte{0x12, 0x34, 0x56, 0x78}},
			want: "12345678",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Byte2Str(tt.args.src)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestBCD2NumberStr(t *testing.T) {
	type args struct {
		bcd []byte
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "case1: long number",
			args: args{bcd: []byte{0x12, 0x34, 0x56, 0x78, 0x90, 0x12}},
			want: "123456789012",
		},
		{
			name: "case1: short number",
			args: args{bcd: []byte{0x12, 0x34}},
			want: "1234",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := bcd2NumberStr(tt.args.bcd)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestNumberStr2BCD(t *testing.T) {
	type args struct {
		number string
	}
	tests := []struct {
		name string
		args args
		want []byte
	}{
		{
			name: "case1: long number",
			args: args{number: "123456789012"},
			want: []byte{0x12, 0x34, 0x56, 0x78, 0x90, 0x12},
		},
		{
			name: "case2: short number",
			args: args{number: "1234"},
			want: []byte{0xff, 0xff, 0x12, 0x34},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := numberStr2BCD(tt.args.number)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestReadByte(t *testing.T) {
	type args struct {
		pkt []byte
		idx *int
	}
	tests := []struct {
		name string
		args args
		want uint8
	}{
		{
			name: "case1: read 0x9a",
			args: args{
				pkt: []byte{0x9a},
				idx: &[]int{0}[0],
			},
			want: 154,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ReadByte(tt.args.pkt, tt.args.idx)
			require.Equal(t, tt.want, got)
			require.Equal(t, tt.args.idx, &[]int{1}[0])
		})
	}
}

func TestWriteByte(t *testing.T) {
	type args struct {
		pkt []byte
		num uint8
	}
	tests := []struct {
		name string
		args args
		want []byte
	}{
		{
			name: "case1: write 0x9a",
			args: args{
				pkt: []byte{},
				num: 154,
			},
			want: []byte{0x9a},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := WriteByte(tt.args.pkt, tt.args.num); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("WriteByte() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReadWord(t *testing.T) {
	type args struct {
		pkt []byte
		idx *int
	}
	tests := []struct {
		name string
		args args
		want uint16
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ReadWord(tt.args.pkt, tt.args.idx); got != tt.want {
				t.Errorf("ReadWord() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWriteWord(t *testing.T) {
	type args struct {
		pkt []byte
		num uint16
	}
	tests := []struct {
		name string
		args args
		want []byte
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := WriteWord(tt.args.pkt, tt.args.num); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("WriteWord() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReadDoubleWord(t *testing.T) {
	type args struct {
		pkt []byte
		idx *int
	}
	tests := []struct {
		name string
		args args
		want uint32
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ReadDoubleWord(tt.args.pkt, tt.args.idx); got != tt.want {
				t.Errorf("ReadDoubleWord() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWriteDoubleWord(t *testing.T) {
	type args struct {
		pkt []byte
		num uint32
	}
	tests := []struct {
		name string
		args args
		want []byte
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := WriteDoubleWord(tt.args.pkt, tt.args.num); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("WriteDoubleWord() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReadString(t *testing.T) {
	type args struct {
		pkt []byte
		idx *int
		n   int
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ReadString(tt.args.pkt, tt.args.idx, tt.args.n); got != tt.want {
				t.Errorf("ReadString() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWriteString(t *testing.T) {
	type args struct {
		pkt []byte
		str string
	}
	tests := []struct {
		name string
		args args
		want []byte
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := WriteString(tt.args.pkt, tt.args.str); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("WriteString() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReadBCD(t *testing.T) {
	type args struct {
		pkt []byte
		idx *int
		n   int
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ReadBCD(tt.args.pkt, tt.args.idx, tt.args.n); got != tt.want {
				t.Errorf("ReadBCD() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWriteBCD(t *testing.T) {
	type args struct {
		pkt []byte
		bcd string
	}
	tests := []struct {
		name string
		args args
		want []byte
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := WriteBCD(tt.args.pkt, tt.args.bcd); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("WriteBCD() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReadGBK(t *testing.T) {
	type args struct {
		pkt []byte
		idx *int
		n   int
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "case1: 京A12345",
			args: args{
				pkt: Str2Byte("BEA9413132333435"),
				idx: &[]int{0}[0],
				n:   8,
			},
			want: "京A12345",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ReadGBK(tt.args.pkt, tt.args.idx, tt.args.n)
			require.Equal(t, tt.want, got)
			require.Equal(t, 8, *tt.args.idx)
		})
	}
}

func TestWriteGBK(t *testing.T) {
	type args struct {
		pkt []byte
		str string
	}
	tests := []struct {
		name string
		args args
		want []byte
	}{
		{
			name: "case1: 京A12345",
			args: args{
				pkt: []byte{},
				str: "京A12345",
			},
			want: Str2Byte("BEA9413132333435"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := WriteGBK(tt.args.pkt, tt.args.str)
			require.Equal(t, tt.want, got)
		})
	}
}
