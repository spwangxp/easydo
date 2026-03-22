import {
  getNotificationInbox,
  getNotificationUnreadCount,
  markNotificationRead,
  markAllNotificationsRead
} from './notification'

export const getMessageList = getNotificationInbox
export const getUnreadCount = getNotificationUnreadCount
export const markAsRead = markNotificationRead
export const markAllAsRead = markAllNotificationsRead
