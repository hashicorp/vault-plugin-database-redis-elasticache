package plugin

import (
	"testing"
)

func Test_generateUserId(t *testing.T) {
	type args struct {
		username string
	}

	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "compliant username",
			args: args{username: "isrole1234eEvyH4mEPcCIT4tCvE131660656371"},
			want: "isrole1234eEvyH4mEPcCIT4tCvE131660656371",
		},
		{
			name: "short username",
			args: args{username: "abcd"},
			want: "abcd",
		},
		{
			name: "username too long",
			args: args{username: "vtokenredisrole1234eEvyH4mEPcCIT4tCvE131660656371"},
			want: "isrole1234eEvyH4mEPcCIT4tCvE131660656371",
		},
		{
			name: "username with non-alphanumeric characters",
			args: args{username: "v_token_redis-role!/$}"},
			want: "vtokenredisrole",
		},
		{
			name: "username starting with a number",
			args: args{username: "1bcd"},
			want: "abcd",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := generateUserId(tt.args.username); got != tt.want {
				t.Errorf("generateUserId() = %v, want %v", got, tt.want)
			}
		})
	}
}
