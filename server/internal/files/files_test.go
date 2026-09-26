package files

import (
	"testing"
	"time"

	"lan-drive/internal/model"
)

// timePtr 取时间地址：model.File.ExpiresAt 是 *time.Time（nil = 永久），
// 测试里大多要造"有期限"的文件，用这个helper 免得每处写临时变量。
func timePtr(t time.Time) *time.Time { return &t }

// 这些测试只覆盖不依赖数据库的纯逻辑：分页规范化、展示字段计算与权限判定。

func TestNormalizePage(t *testing.T) {
	cases := []struct {
		inPage, inSize, wantPage, wantSize int
	}{
		{0, 0, 1, 20},
		{-5, -5, 1, 20},
		{1, 1, 1, 1},
		{3, 50, 3, 50},
		{2, 200, 2, 200},
		{2, 5000, 2, 200}, // 上限 200，防止一次拉爆数据库
	}
	for _, c := range cases {
		p, s := normalizePage(c.inPage, c.inSize)
		if p != c.wantPage || s != c.wantSize {
			t.Fatalf("normalizePage(%d, %d) = (%d, %d)，期望 (%d, %d)",
				c.inPage, c.inSize, p, s, c.wantPage, c.wantSize)
		}
	}
}

func TestDecorateComputesDaysLeftAndEditability(t *testing.T) {
	svc := &Service{}
	owner := &model.User{ID: 5, Role: model.RoleUser, Enabled: true}
	other := &model.User{ID: 6, Role: model.RoleUser, Enabled: true}
	admin := &model.User{ID: 9, Role: model.RoleAdmin, Enabled: true}

	f := &model.File{
		ID:        1,
		OwnerID:   5,
		Status:    model.StatusActive,
		ExpiresAt: timePtr(time.Now().UTC().Add(72 * time.Hour)),
	}

	// 属主：可编辑，剩余天数约 3 天
	got := svc.decorate(f, owner)
	if !got.IsMine || !got.CanEdit {
		t.Fatalf("属主应可编辑: %+v", got)
	}
	if got.DaysLeft != 3 {
		t.Fatalf("剩余天数 = %d，期望 3", got.DaysLeft)
	}

	// 他人：可见可下载，但不可编辑
	got = svc.decorate(f, other)
	if got.IsMine || got.CanEdit {
		t.Fatalf("非属主不应可编辑: %+v", got)
	}

	// 管理员：可管理任意文件
	got = svc.decorate(f, admin)
	if got.IsMine {
		t.Fatalf("管理员对他人文件 IsMine 应为 false")
	}
	if !got.CanEdit {
		t.Fatalf("管理员应可管理任意文件")
	}

	// 已删除文件：连属主也不能再编辑
	trashed := *f
	trashed.Status = model.StatusTrashed
	got = svc.decorate(&trashed, owner)
	if got.CanEdit {
		t.Fatalf("回收站中的文件不应可编辑: %+v", got)
	}

	// 已过期文件：剩余天数为负
	expired := *f
	expired.ExpiresAt = timePtr(time.Now().UTC().Add(-48 * time.Hour))
	got = svc.decorate(&expired, owner)
	if got.DaysLeft >= 0 {
		t.Fatalf("已过期文件剩余天数应为负，实际 %d", got.DaysLeft)
	}

	// 未登录：既非本人也不可编辑
	got = svc.decorate(f, nil)
	if got.IsMine || got.CanEdit {
		t.Fatalf("未登录不应有任何编辑权限: %+v", got)
	}
}

func TestCheckCanModify(t *testing.T) {
	svc := &Service{}
	owner := &model.User{ID: 1, Role: model.RoleUser}
	other := &model.User{ID: 2, Role: model.RoleUser}
	admin := &model.User{ID: 3, Role: model.RoleAdmin}

	active := &model.File{OwnerID: 1, Status: model.StatusActive}
	trashed := &model.File{OwnerID: 1, Status: model.StatusTrashed}

	// 属主可改自己的有效文件
	if err := svc.checkCanModify(active, owner); err != nil {
		t.Fatalf("属主应可修改: %v", err)
	}
	// 他人不可改
	if err := svc.checkCanModify(active, other); err == nil {
		t.Fatalf("他人不应可修改")
	}
	// 属主不可改已被删除的文件
	if err := svc.checkCanModify(trashed, owner); err == nil {
		t.Fatalf("已删除文件不应可修改")
	}
	// 管理员可改任意状态
	if err := svc.checkCanModify(trashed, admin); err != nil {
		t.Fatalf("管理员应可修改: %v", err)
	}
	// 未登录
	if err := svc.checkCanModify(active, nil); err == nil {
		t.Fatalf("未登录不应可修改")
	}
}

func TestStatusConstantsAreStable(t *testing.T) {
	// 状态值直接落库，改动会造成历史数据不可读，因此锁定字面量。
	if model.StatusActive != "active" || model.StatusTrashed != "trashed" {
		t.Fatalf("文件状态常量被改动: %q / %q", model.StatusActive, model.StatusTrashed)
	}
	// 角色同理（JWT 载荷与数据库均使用字符串）。
	if model.RoleAdmin != "admin" || model.RoleUser != "user" {
		t.Fatalf("角色常量被改动")
	}
	// 普通用户与管理员的权限判定必须区分开。
	if (&model.User{Role: model.RoleUser}).IsAdmin() {
		t.Fatalf("普通用户不应被判为管理员")
	}
	if !(&model.User{Role: model.RoleAdmin}).IsAdmin() {
		t.Fatalf("管理员应被判为管理员")
	}
}

func TestErrForbiddenIsDistinct(t *testing.T) {
	// handler 依赖 errors.Is 把越权映射为 403，这里确认错误可被识别。
	if ErrForbidden == nil || ErrForbidden.Error() == "" {
		t.Fatalf("ErrForbidden 必须有可读消息")
	}
}
