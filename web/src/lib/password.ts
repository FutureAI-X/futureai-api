// 手动设置密码时的最低强度规则。
// 必须与后端 common.ValidatePasswordStrength 保持一致——后端是最终防线，
// 这里的校验只是为了尽早给出提示。
export const PASSWORD_MIN_LENGTH = 8
export const PASSWORD_MIN_CLASSES = 3

export function validatePasswordStrength(password: string): string | null {
  if (password.length < PASSWORD_MIN_LENGTH) {
    return `密码长度不能少于${PASSWORD_MIN_LENGTH}位`
  }

  const classes = [/[A-Z]/, /[a-z]/, /[0-9]/, /[^A-Za-z0-9]/].filter((re) =>
    re.test(password)
  ).length

  if (classes < PASSWORD_MIN_CLASSES) {
    return `密码需至少包含大写字母、小写字母、数字、符号中的${PASSWORD_MIN_CLASSES}类`
  }

  return null
}
