import * as React from 'react'
import Banner from '../bottom-banner/container'
import HeaderArea from '../header-area/container.native'
import InputArea from '../input-area/container'
import ListArea from '../list-area/container'
import {Box, Box2, LoadingLine, Text} from '../../../common-adapters'
import {Props} from '.'
import ThreadLoadStatus from '../load-status/container'
import PinnedMessage from '../pinned-message/container'
import {GatewayDest} from 'react-gateway'
import InvitationToBlock from '../../blocking/invitation-to-block'
import * as Styles from '../../../styles'

const Offline = () => (
  <Box
    style={{
      ...Styles.globalStyles.flexBoxCenter,
      backgroundColor: Styles.globalColors.greyDark,
      paddingBottom: Styles.globalMargins.tiny,
      paddingLeft: Styles.globalMargins.medium,
      paddingRight: Styles.globalMargins.medium,
      paddingTop: Styles.globalMargins.tiny,
      width: '100%',
    }}
  >
    <Text center={true} type="BodySmallSemibold">
      Couldn't load all chat messages due to network connectivity. Retrying...
    </Text>
  </Box>
)

const Conversation = React.memo((props: Props) => (
  <Box2 direction="vertical" style={styles.innerContainer}>
    <Box2 direction="vertical" fullWidth={true} fullHeight={true}>
      {props.threadLoadedOffline && <Offline />}
      {!Styles.isTablet && <HeaderArea conversationIDKey={props.conversationIDKey} />}
      <Box2 direction="vertical" fullWidth={true} style={styles.innerContainer}>
        <ThreadLoadStatus conversationIDKey={props.conversationIDKey} />
        <PinnedMessage conversationIDKey={props.conversationIDKey} />
        <ListArea
          scrollListDownCounter={props.scrollListDownCounter}
          scrollListToBottomCounter={props.scrollListToBottomCounter}
          scrollListUpCounter={props.scrollListUpCounter}
          onFocusInput={props.onFocusInput}
          conversationIDKey={props.conversationIDKey}
        />
        {props.showLoader && <LoadingLine />}
      </Box2>
      <InvitationToBlock conversationID={props.conversationIDKey} />
      <Banner conversationIDKey={props.conversationIDKey} />
      <InputArea
        focusInputCounter={props.focusInputCounter}
        jumpToRecent={props.jumpToRecent}
        onRequestScrollDown={props.onRequestScrollDown}
        onRequestScrollToBottom={props.onRequestScrollToBottom}
        onRequestScrollUp={props.onRequestScrollUp}
        conversationIDKey={props.conversationIDKey}
      />
    </Box2>
    <GatewayDest name="convOverlay" component={Box} />
  </Box2>
))

const styles = Styles.styleSheetCreate(
  () =>
    ({
      container: Styles.platformStyles({
        isTablet: {
          flex: 1,
          position: 'relative',
        },
      }),
      innerContainer: {
        flex: 1,
        position: 'relative',
      },
    } as const)
)

export default Conversation
